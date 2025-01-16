package main

import (
	"crypto/tls"

	"git.2mb.codes/~cmb/barnard/config"
	"git.2mb.codes/~cmb/barnard/gumble/gumble"
	"git.2mb.codes/~cmb/barnard/gumble/gumbleopenal"
	"git.2mb.codes/~cmb/barnard/uiterm"
)

type Barnard struct {
	Config     *gumble.Config
	UserConfig *config.Config
	Hotkeys    *config.Hotkeys
	Client     *gumble.Client

	Address   string
	TLSConfig tls.Config

	Stream    *gumbleopenal.Stream
	Tx        bool
	Connected bool

	Ui              *uiterm.Ui
	UiOutput        uiterm.Textview
	UiInput         uiterm.Textbox
	UiStatus        uiterm.Label
	UiTree          uiterm.Tree
	UiInputStatus   uiterm.Label
	SelectedChannel *gumble.Channel
	selectedUser    *gumble.User

	notifyChannel chan []string

	exitStatus  int
	exitMessage string
}
