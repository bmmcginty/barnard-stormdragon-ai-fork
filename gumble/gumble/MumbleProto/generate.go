//go:generate sh -c "curl -L -sS -o Mumble.proto https://raw.githubusercontent.com/mumble-voip/mumble/master/src/Mumble.proto && protoc --go_out=. --go_opt=paths=source_relative --go_opt=MMumble.proto=git.stormux.org/storm/barnard/gumble/gumble/MumbleProto Mumble.proto && rm -f Mumble.proto"
package MumbleProto
