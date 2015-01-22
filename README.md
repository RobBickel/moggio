# mog

An audio player written in Go.

# goals

1. JSON API. Playlists easily managed with a web browser, and global OS keyboard shortcuts can access functions needed from the media keys (next, pause, play).
1. Support for the following formats:
   * wav
   * ogg
   * flac
   * mp3
   * spc (Super Nintendo)
   * nsf, nsfe (Nintendo)
1. Support for the Google Music protocol.
1. Support for archive files (.zip, .rar, .nsf, etc.).
1. Pure Go except for sound driver interfaces.
1. Runs on Windows, Linux, Mac OSX.

# current status

- When run, can play your MP3 and NSF files from Dropbox, Google Drive, Soundcloud, local hard drive, and Google Music.
- UI is improving but usable enough.
- Connect to [http://localhost:6601](http://localhost:6601) to see the UI.
- Dropbox, Google Drive, and Soundcloud use my API keys by default, but can be changed on the command line. Their oauth redirects go to `localhost:6601`.

# todo

- NSF codec working but needs improvement: bandpass filter on output; square wave 1 needs slightly tweaked behavior wrt square wave 2; needs more tests, especially some timing tests
- google music can login, fetch playlists and request an MP3 stream; needs to support the non-android protocol to download the device IDs, not sure how the auth works for it
- MP3 support is working but needs ID3 support≈
