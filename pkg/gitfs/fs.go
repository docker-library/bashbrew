package gitfs

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	goGit "github.com/go-git/go-git/v5"
	goGitPlumbing "github.com/go-git/go-git/v5/plumbing"
	goGitPlumbingFileMode "github.com/go-git/go-git/v5/plumbing/filemode"
	goGitPlumbingObject "github.com/go-git/go-git/v5/plumbing/object"
)

// https://github.com/go-git/go-git/issues/296

// TODO something more clever for directories

func CommitHash(repo *goGit.Repository, commit string) (fs.FS, error) {
	gitCommit, err := repo.CommitObject(goGitPlumbing.NewHash(commit))
	if err != nil {
		return nil, err
	}
	return gitFS{
		commit: gitCommit,
	}, nil
}

// https://pkg.go.dev/io/fs#FS
type gitFS struct {
	commit *goGitPlumbingObject.Commit
}

// apparently symlinks in "io/fs" are still a big TODO (https://github.com/golang/go/issues/49580, https://github.com/golang/go/issues/45470, etc related issues); all the existing interfaces assume symlinks don't exist
//
// if the File object passed to this function represents a symlink, this returns the (resolved) path that should be looked up instead; only relative symlinks are supported (and attempts to escape the repository with too many "../" *should* result in an error -- this is a convenience/sanity check, not a security boundary; subset of https://pkg.go.dev/io/fs#ValidPath)
//
// otherwise, it will return the empty string and nil
func resolveSymlink(f *goGitPlumbingObject.File) (target string, err error) {
	if f.Mode != goGitPlumbingFileMode.Symlink {
		return "", nil
	}

	target, err = f.Contents()
	if err != nil {
		return "", err
	}

	if target == "" {
		return "", fmt.Errorf("unexpected: empty symlink %q", f.Name)
	}

	if path.IsAbs(target) {
		return "", fmt.Errorf("unsupported: %q is an absolute symlink (%q)", f.Name, target)
	}

	target = path.Join(path.Dir(f.Name), target)

	if strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("unsupported: %q is a relative symlink outside the tree (%q)", f.Name, target)
	}

	return target, nil
}

// https://pkg.go.dev/io/fs#FS
func (fs gitFS) Open(name string) (fs.File, error) {
	f, err := fs.commit.File(name)
	if err != nil {
		// TODO if it's file-not-found, we need to check whether it's a directory
		return nil, err
	}

	if target, err := resolveSymlink(f); err != nil {
		return nil, err
	} else if target != "" {
		return fs.Open(target)
	}

	reader, err := f.Reader()
	if err != nil {
		return nil, err
	}

	return gitFSFile{
		stat: gitFSFileInfo{
			file: f,
		},
		reader: reader,
	}, nil
}

// https://pkg.go.dev/io/fs#StatFS
func (fs gitFS) Stat(name string) (fs.FileInfo, error) {
	f, err := fs.commit.File(name)
	if err != nil {
		return nil, err
	}

	if target, err := resolveSymlink(f); err != nil {
		return nil, err
	} else if target != "" {
		return fs.Stat(target)
	}

	return gitFSFileInfo{
		file: f,
	}, nil
}

// https://pkg.go.dev/io/fs#File
type gitFSFile struct {
	stat fs.FileInfo
	reader io.ReadCloser
}
func (f gitFSFile) Stat() (fs.FileInfo, error) {
	return f.stat, nil
}
func (f gitFSFile) Read(b []byte) (int, error) {
	return f.reader.Read(b)
}
func (f gitFSFile) Close() error {
	return f.reader.Close()
}

type gitFSFileInfo struct {
	file *goGitPlumbingObject.File
}

// base name of the file
func (fi gitFSFileInfo) Name() string {
	return path.Base(fi.file.Name)
}

// length in bytes for regular files; system-dependent for others
func (fi gitFSFileInfo) Size() int64 {
	return fi.file.Size
}

// file mode bits
func (fi gitFSFileInfo) Mode() fs.FileMode {
	// https://pkg.go.dev/github.com/go-git/go-git/v5@v5.4.2/plumbing/filemode#FileMode
	// https://pkg.go.dev/io/fs#FileMode
	switch fi.file.Mode {
	case goGitPlumbingFileMode.Regular:
		return 0644
	case goGitPlumbingFileMode.Symlink:
		return 0644 | fs.ModeSymlink
	case goGitPlumbingFileMode.Executable:
		return 0755
	case goGitPlumbingFileMode.Dir:
		return 0755 | fs.ModeDir
	}
	return 0 | fs.ModeIrregular // TODO what to do for files whose types we don't support? 😬
}

// modification time
func (fi gitFSFileInfo) ModTime() time.Time {
	return time.Time{} // TODO maybe pass down whichever is more recent of commit.Author.When vs commit.Committer.When ?
}

// abbreviation for Mode().IsDir()
func (fi gitFSFileInfo) IsDir() bool {
	return fi.file.Mode == goGitPlumbingFileMode.Dir
}

// underlying data source (can return nil)
func (fi gitFSFileInfo) Sys() interface{} {
	return fi.file
}
