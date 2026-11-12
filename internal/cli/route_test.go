package cli

import "testing"

func TestWantsCLI(t *testing.T) {
	for _, args := range [][]string{
		{"accounts", "list"},
		{"--help"},
		{"--output", "json", "version"},
		{"--output=json"},
		{"--bogus"},
		{"-x"},
	} {
		if !WantsCLI(args) {
			t.Errorf("%v should reach the command line", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"--data", "/tmp/subman"},
		{"--data=/tmp/subman"},
		{"-data", "/tmp/subman"},
	} {
		if WantsCLI(args) {
			t.Errorf("%v should open the window", args)
		}
	}
}
