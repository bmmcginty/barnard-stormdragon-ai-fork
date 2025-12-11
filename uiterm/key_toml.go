package uiterm

// MarshalText implements the encoding.TextMarshaler interface for Key
// This allows TOML to serialize Key values as strings
func (i Key) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for Key
// This allows TOML to deserialize strings back into Key values
func (i *Key) UnmarshalText(data []byte) error {
	var err error
	*i, err = KeyString(string(data))
	return err
}
