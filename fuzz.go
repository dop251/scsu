// +build gofuzz

package scsu

import (
	"bytes"
	"errors"
	"fmt"
)

func Fuzz(data []byte) int {
	var b bytes.Buffer
	e := NewEncoder(&b)
	s := string(data)
	_, err := e.Encode(StrictStringRuneSource(s))
	validUTF := true
	if err == nil {
		b1 := bytes.NewBuffer(b.Bytes())
		d := NewDecoder(b1)
		s1, err := d.ReadString()
		if err != nil {
			panic(err)
		}
		if s1 != s {
			panic(fmt.Sprintf("Values do not match: '%s', '%s'", s1, s))
		}
	} else if errors.Is(err, ErrInvalidUTF8) {
		validUTF = false
	} else {
		panic(err)
	}

	// try as an input for decoder
	_, err = Decode(data)

	if err == nil || validUTF {
		return 1
	}

	return 0
}
