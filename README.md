iii
===

iii - ii Improved   

  
suckless version of suckless ii, but with horriblehorrible Golang.  
http://tools.suckless.org/ii/
  
iii (and ii) is a filsystem-based IRC client, using files and FIFO pipes to
communicate with the IRC server. It allows the creation of simple scripts and
bots.

The goal is to have 1:1 feature parity with ii, and some additional features:
* TLS Support
* Server reconnection & channel rejoin


```
→ iii  --help
Usage of ./iii:
  -f string
    	Specify a default real name (default "ii Improved")
  -i string
    	Specify a path for the IRC connection (default "~/irc")
  -k string
    	Specify a environment variable for your IRC password (default "IIPASS")
  -n string
    	Specify a default nick (default "iii")
  -p string
    	Server port (default 6667, SSL default 6697)
  -s string
    	Specify server (default "irc.freenode.net")
  -tls
    	Use TLS for the connection (default false)

```

```
→ tree irc
irc
└── irc.hackint.org
    ├── #buf
    │   ├── in
    │   └── out
    ├── foxboron
    │   ├── in
    │   └── out
    ├── in
    └── out

3 directories, 6 files
```
