package stripper

import (
	"bufio"
	"bytes"
	"io"
	"unicode"
)

type LeadingWhitespaceStripper struct {
	// it's on like
	donkeyKong bool

	r   *bufio.Reader
	buf bytes.Buffer
}

func NewLeadingWhitespaceStripper(r io.Reader) *LeadingWhitespaceStripper {
	return &LeadingWhitespaceStripper{
		donkeyKong: true,

		r: bufio.NewReader(r),
	}
}

func (r *LeadingWhitespaceStripper) Read(p []byte) (int, error) {
	if r.donkeyKong {
		for {
			char, _, err := r.r.ReadRune()
			if err != nil {
				return 0, err
			}
			if !unicode.IsSpace(char) {
				r.donkeyKong = false
				err = r.r.UnreadRune()
				if err != nil {
					return 0, err
				}
				break
			}
		}
	}
	return r.r.Read(p)
}
