package tarscrub

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
)

// TODO create an io/fs that parses a Dockerfile in an io/fs and effectively "filters" the io/fs to only return/include files that are used by that Dockerfile 👀

// takes a tar header object and "scrubs" it (uid/gid zeroed, timestamps zeroed)
func ScrubHeader(hdr *tar.Header) *tar.Header {
	return &tar.Header{
		Typeflag: hdr.Typeflag,
		Name:     hdr.Name,
		Linkname: hdr.Linkname,
		Size:     hdr.Size,
		Mode:     hdr.Mode,
		Devmajor: hdr.Devmajor,
		Devminor: hdr.Devminor,
	}
}

// this writes a "scrubbed" tarball to the given io.Writer (uid/gid zeroed, timestamps zeroed)
func WriteTar(f fs.FS, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Flush() // note: flush instead of close to avoid the empty block at EOF

	// https://github.com/golang/go/blob/go1.22rc1/src/archive/tar/writer.go#L408-L443
	// https://cs.opensource.google/go/go/+/go1.22rc1:src/archive/tar/writer.go;l=411
	return fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("%q: %w", path, err)
		}
		// TODO add more context to more errors

		if path == "." {
			// skip "." to match "git archive" behavior -- TODO this should be optional somehow
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path
		if info.IsDir() {
			hdr.Name += "/"
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			// https://github.com/golang/go/issues/49580 ("type ReadLinkFS interface")
			if readlinkFS, ok := f.(interface {
				ReadLink(name string) (string, error)
			}); ok {
				hdr.Linkname, err = readlinkFS.ReadLink(path)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("filesystem contains symlinks but does not implement ReadLinkFS (needed for symlink %q)", path)
			}
		}

		newHdr := ScrubHeader(hdr)
		if err := tw.WriteHeader(newHdr); err != nil {
			return err
		}

		if info.IsDir() || hdr.Linkname != "" {
			return nil
		}

		file, err := f.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tw, file)
		return err
	})
}
