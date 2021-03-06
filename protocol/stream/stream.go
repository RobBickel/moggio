package stream

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mjibson/moggio/codec"
	"github.com/mjibson/moggio/codec/mpa"
	"github.com/mjibson/moggio/protocol"
	"golang.org/x/oauth2"
)

func init() {
	protocol.Register("stream", []string{"URL"}, New, reflect.TypeOf(&Stream{}))
	gob.Register(new(Stream))
}

func New(params []string, token *oauth2.Token) (protocol.Instance, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("expected one parameter")
	}
	addr, err := url.Parse(params[0])
	if err != nil {
		return nil, err
	}
	s := Stream{
		Orig: params[0],
		Host: addr.Host,
	}
	s.Refresh()
	resp, err := s.get()
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	return &s, nil
}

// tryPLS checks if u is a URL to a .pls or .m3u file. If it is, it returns the
// first File entry of as target and first Title entry as name.
func tryPLS(u string) (target, name string) {
	target = u
	resp, err := http.Get(u)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	i := 0
	// Attempt to parse as PLS.
	for sc.Scan() {
		if i > 5 {
			break
		}
		i++
		sp := strings.SplitN(sc.Text(), "=", 2)
		if len(sp) != 2 {
			continue
		}
		if strings.HasPrefix(sp[0], "File") {
			_, err := url.Parse(sp[1])
			if err != nil {
				return
			}
			target = sp[1]
		} else if strings.HasPrefix(sp[0], "Title") {
			name = sp[1]
		}
		if target != u && name != "" {
			return
		}
	}
	// Attempt to parse as M3U.
	sc = bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		t := sc.Text()
		if strings.HasPrefix(t, "#") {
			continue
		}
		if _, err := url.Parse(t); err == nil {
			target = t
			return
		}
	}
	return
}

type Stream struct {
	Orig           string
	URL            string
	Host           string
	Name           string
	metaint, count int
	body           io.ReadCloser
	title          string
	songtitle      string
}

type dialer struct {
	*net.Dialer
}

type conn struct {
	net.Conn
	read bool
}

// Read modifies the first line of an ICY stream response,
// if needed, to conform to Go's HTTP version requirements:
// http://golang.org/pkg/net/http/#ParseHTTPVersion.
func (c *conn) Read(b []byte) (n int, err error) {
	if !c.read {
		const headerICY = "ICY"
		const headerHTTP = "HTTP/1.1"
		// Hold 5 bytes because "HTTP/1.1" is 5 bytes longer than "ICY".
		n, err := c.Conn.Read(b[:len(b)+len(headerICY)-len(headerHTTP)])
		if bytes.HasPrefix(b, []byte(headerICY)) {
			copy(b[len(headerHTTP):], b[len(headerICY):])
			copy(b, []byte(headerHTTP))
		}
		c.read = true
		return n, err
	}
	return c.Conn.Read(b)
}

func (d *dialer) Dial(network, address string) (net.Conn, error) {
	c, err := d.Dialer.Dial(network, address)
	cn := conn{
		Conn: c,
	}
	return &cn, err
}

var client = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&dialer{
			Dialer: &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			},
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

func (s *Stream) get() (*http.Response, error) {
	req, err := http.NewRequest("GET", s.URL, nil)
	if err != nil {
		panic(err)
		log.Fatal(err)
	}
	req.Header.Add("Icy-MetaData", "1")
	log.Println("stream open", req.URL)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("stream status: %v", resp.Status)
	}
	s.metaint, err = strconv.Atoi(resp.Header.Get("Icy-Metaint"))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Stream) info() *codec.SongInfo {
	return &codec.SongInfo{
		Title: s.Name,
		Album: s.Host,
	}
}

func (s *Stream) Key() string {
	return s.Orig
}

func (s *Stream) List() (protocol.SongList, error) {
	return protocol.SongList{
		codec.ID(s.URL): s.info(),
	}, nil
}

func (s *Stream) Refresh() (protocol.SongList, error) {
	u, name := tryPLS(s.Orig)
	if name == "" {
		name = s.Orig
	}
	s.URL = u
	s.Name = name
	return s.List()
}

func (s *Stream) Info(codec.ID) (*codec.SongInfo, error) {
	i := s.info()
	i.SongTitle = s.songtitle
	return i, nil
}

func (s *Stream) GetSong(codec.ID) (codec.Song, error) {
	return mpa.NewSong(s.reader())
}

func (s *Stream) reader() codec.Reader {
	return func() (io.ReadCloser, int64, error) {
		resp, err := s.get()
		if err != nil {
			return nil, 0, err
		}
		s.Close()
		s.body = resp.Body
		s.songtitle = ""
		return s, 0, nil
	}
}

var titleRE = regexp.MustCompile("StreamTitle='(.*?)';")

func (s *Stream) Read(p []byte) (n int, err error) {
	if s.metaint == 0 {
		return s.body.Read(p)
	}
	l := s.metaint - s.count
	if len(p) > l {
		p = p[:l]
	}
	n, err = s.body.Read(p)
	s.count += n
	if s.count == s.metaint {
		s.count = 0
		mlen := make([]byte, 1)
		if _, err := io.ReadFull(s.body, mlen); err != nil {
			return n, err
		}
		meta := make([]byte, int(mlen[0])*16)
		if _, err := io.ReadFull(s.body, meta); err != nil {
			return n, err
		}
		matches := titleRE.FindSubmatch(meta)
		if len(matches) == 2 {
			s.songtitle = string(matches[1])
		}
	}
	return
}

func (s *Stream) Close() error {
	var err error
	if s.body != nil {
		err = s.body.Close()
	}
	s.body = nil
	s.count = 0
	s.title = ""
	return err
}
