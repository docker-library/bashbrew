package stripper_test

import (
	"io"
	"os"
	"strings"

	"github.com/docker-library/go-dockerlibrary/pkg/stripper"
)

func ExampleCommentStripper() {
	r := strings.NewReader(`
# opening comment
a: b
# comment!
c: d # not a comment

# another cheeky comment
e: f
`)

	comStrip := stripper.NewCommentStripper(r)

	io.Copy(os.Stdout, comStrip)

	// Output:
	// a: b
	// c: d # not a comment
	//
	// e: f
}
