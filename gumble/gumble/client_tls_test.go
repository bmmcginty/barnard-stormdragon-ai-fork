package gumble

import "testing"

func TestTLSServerNameUsesAddressHost(t *testing.T) {
	for _, test := range []struct {
		address string
		want    string
	}{
		{"mumble.example:64738", "mumble.example"},
		{"[2001:db8::1]:64738", "2001:db8::1"},
	} {
		got, err := tlsServerName(test.address)
		if err != nil {
			t.Errorf("tlsServerName(%q): %v", test.address, err)
			continue
		}
		if got != test.want {
			t.Errorf("tlsServerName(%q) = %q, want %q", test.address, got, test.want)
		}
	}
}

func TestTLSServerNameRejectsAddressWithoutHost(t *testing.T) {
	if _, err := tlsServerName(":64738"); err == nil {
		t.Fatal("tlsServerName accepted an empty host")
	}
}
