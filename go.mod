module git.stormux.org/storm/barnard

go 1.25.0

require (
	al.essio.dev/pkg/shellescape v1.6.0
	github.com/hraban/opus v0.0.0-20251117090126-c76ea7e21bf3
	github.com/kennygrant/sanitize v1.2.4
	github.com/nsf/termbox-go v1.1.1
	github.com/pelletier/go-toml/v2 v2.3.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	golang.org/x/net v0.54.0 // indirect
)

replace git.stormux.org/storm/barnard/gumble/go-openal => ./gumble/go-openal
