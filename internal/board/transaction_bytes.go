package board

import (
	"encoding/json"
	"unicode/utf8"
)

// JSON strings replace invalid UTF-8. Binary attachments therefore use base64
// fields in the journal, while existing textual operation records stay readable.
func (f FileChange) MarshalJSON() ([]byte, error) {
	type plain FileChange
	if utf8.ValidString(f.After) && (f.Before == nil || utf8.ValidString(*f.Before)) {
		return json.Marshal(plain(f))
	}
	var before *[]byte
	if f.Before != nil {
		b := []byte(*f.Before)
		before = &b
	}
	return json.Marshal(struct {
		Path   string  `json:"path"`
		Binary bool    `json:"binary"`
		Before *[]byte `json:"before_bytes,omitempty"`
		After  []byte  `json:"after_bytes"`
	}{f.Path, true, before, []byte(f.After)})
}
func (f *FileChange) UnmarshalJSON(data []byte) error {
	type plain FileChange
	var header struct {
		Binary bool `json:"binary"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if !header.Binary {
		return json.Unmarshal(data, (*plain)(f))
	}
	var raw struct {
		Path   string  `json:"path"`
		Before *[]byte `json:"before_bytes"`
		After  []byte  `json:"after_bytes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Path, f.After, f.Before = raw.Path, string(raw.After), nil
	if raw.Before != nil {
		b := string(*raw.Before)
		f.Before = &b
	}
	return nil
}
