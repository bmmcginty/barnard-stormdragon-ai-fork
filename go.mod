module git.stormux.org/storm/barnard

go 1.25.0

require (
	al.essio.dev/pkg/shellescape v1.6.0
	github.com/hraban/opus v0.0.0-20260708213942-bde8e4304501
	github.com/kennygrant/sanitize v1.2.4
	github.com/nsf/termbox-go v1.1.1
	github.com/pelletier/go-toml/v2 v2.4.3
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	golang.org/x/net v0.57.0 // indirect
)

replace git.stormux.org/storm/barnard/gumble/go-openal => ./gumble/go-openal
