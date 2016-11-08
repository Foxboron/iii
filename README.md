iii
===

iii - ii Improved   

  
suckless version of suckless ii, but with horriblehorrible Golang.  
Goal is to support TLS, like sane people.  
http://tools.suckless.org/ii/

Goal is 1:1 parity featurewise, but below 500 lines of bloody code.


```
→ iii  --help
Usage of ./iii:
  -f string
    	Speciy a default real name (default "ii Improved")
  -i string
    	Specify a path for the IRC connection (default "~/irc")
  -k string
    	Specify a environment variable for your IRC password (default "IIPASS")
  -n string
    	Speciy a default nick (default "iii")
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
├── in
├── irc.hackint.org
│   ├── #buf
│   │   ├── in
│   │   └── out
│   ├── foxboron
│   │   ├── in
│   │   └── out
│   ├── in
│   └── out
└── out

3 directories, 8 files
```


WIP - Please don't be mad at me
