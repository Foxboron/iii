package main

import (
	"fmt"
	"testing"
)

func sliceEqual(s1, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}

	for i, _ := range s1 {
		if s1[i] != s2[i] {
			return false
		}
	}

	return true
}

func diffParsed(got, want Parsed) error {
	if got.nick != want.nick {
		return fmt.Errorf(".nick = %q, want %q", got.nick, want.nick)
	}
	if got.uinf != want.uinf {
		return fmt.Errorf(".uinf = %q, want %q", got.uinf, want.uinf)
	}
	if got.cmd != want.cmd {
		return fmt.Errorf(".cmd = %q, want %q", got.cmd, want.cmd)
	}
	if got.channel != want.channel {
		return fmt.Errorf(".channel = %q, want %q", got.channel,
			want.channel)
	}
	if !sliceEqual(got.args, want.args) {
		return fmt.Errorf(".args = %q, want %q", got.args, want.args)
	}

	return nil
}

func TestParse(t *testing.T) {
	tests := []struct {
		input string
		want  Parsed
	}{
		{":dmr!dmr@bell-labs.com QUIT",
			Parsed{nick: "dmr",
				uinf:    "dmr@bell-labs.com",
				cmd:     "QUIT",
				channel: "",
				args:    []string{},
			},
		},
		{":oR!xor@2001:2002:51e2:7ba1:e216:8860:f727:279b KICK #lehrer" +
			" squelch :too many references",
			Parsed{nick: "oR",
				uinf:    "xor@2001:2002:51e2:7ba1:e216:8860:f727:279b",
				cmd:     "KICK",
				channel: "#lehrer",
				args: []string{"#lehrer", "squelch",
					"too many references"},
			},
		},
		{":Bobo PRIVMSG #Dessert chocolate",
			Parsed{nick: "Bobo",
				uinf:    "",
				cmd:     "PRIVMSG",
				channel: "#dessert",
				args:    []string{"#Dessert", "chocolate"},
			},
		},
		{":squelch!~sql@foonly.xyz TOPIC #lehrer :Smut & nothing but!",
			Parsed{nick: "squelch",
				uinf:    "~sql@foonly.xyz",
				cmd:     "TOPIC",
				channel: "#lehrer",
				args:    []string{"#lehrer", "Smut & nothing but!"},
			},
		},
		{"ERROR :Closing Link: xxx.xxx.xxx.xxx (Client Quit)",
			Parsed{nick: "",
				uinf: "",
				cmd: "ERROR",
				channel: "",
				args: []string{"Closing Link: xxx.xxx.xxx.xxx (Client Quit)"},
			},
		},
	}

	// needed for parse()
	for _, s := range glMsgs {
		globalCmds[s] = struct{}{}
	}

	for _, test := range tests {
		test.want.raw = test.input
		got, err := parse(test.input)
		if err != nil {
			t.Errorf(`%q parsed error: %v`, test.input, err)
		}

		if err = diffParsed(got, test.want); err != nil {
			t.Errorf(`parse %q: %v`, test.input, err)
		}
	}
}
