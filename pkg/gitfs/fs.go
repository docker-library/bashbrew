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
	goGitPlumbingStorer "github.com/go-git/go-git/v5/plumbing/storer"
)

// https://github.com/go-git/go-git/issues/296

func CommitTime(commit *goGitPlumbingObject.Commit) time.Time {
	if commit.Committer.When.After(commit.Author.When) {
		return commit.Committer.When
	} else {
		return commit.Author.When
	}
}

func CommitHash(repo *goGit.Repository, commit string) (fs.FS, error) {
	gitCommit, err := repo.CommitObject(goGitPlumbing.NewHash(commit))
	if err != nil {
		return nil, err
	}
	tree, err := gitCommit.Tree()
	if err != nil {
		return nil, err
	}
	f := &gitFS{
		storer: repo.Storer,
		tree:   tree,
		name:   ".",
		Mod:    CommitTime(gitCommit),
	}
	f.root = f
	return gitFSFS{
		gitFS: f,
	}, nil
}

// https://pkg.go.dev/io/fs#FS
// This exists *only* because we cannot create a single object that concurrently implements *both* fs.FS *and* fs.File (Stat(string) vs Stat()).
type gitFSFS struct {
	*gitFS
}

// https://pkg.go.dev/io/fs#File
// https://pkg.go.dev/io/fs#FileInfo
// https://pkg.go.dev/io/fs#DirEntry
type gitFS struct {
	root *gitFS // used so we can rewind back to the root if we need to (see symlink handling code; should *only* be set in CommitHash / constructors)

	storer goGitPlumbingStorer.EncodedObjectStorer
	tree   *goGitPlumbingObject.Tree
	entry  *goGitPlumbingObject.TreeEntry // might be nil ("." at the top-level of the repo)

	Mod time.Time

	// cached values
	name string // full path from the repository root
	size int64  // Tree.Size value for non-directories (more efficient than opening/reading the blob)

	// state for "Open" objects
	reader io.ReadCloser                   // only set for an "Open" file
	walker *goGitPlumbingObject.TreeWalker // only set for an "Open" directory
}

// clones just the load-bearing bits (basically clearing anything that's "state"
func (f gitFS) clone() *gitFS {
	f.reader = nil
	f.walker = nil
	return &f
}

// if our entry is a symlink, this returns the target of it
func (f gitFS) readLink() (bool, string, error) {
	if f.entry == nil || f.entry.Mode != goGitPlumbingFileMode.Symlink {
		return false, "", nil
	}

	file, err := f.tree.TreeEntryFile(f.entry)
	if err != nil {
		return true, "", fmt.Errorf("TreeEntryFile(%q): %w", f.name, err)
	}

	target, err := file.Contents()
	return true, target, err
}

// symlinks in "io/fs" are still a big TODO (https://github.com/golang/go/issues/49580, https://github.com/golang/go/issues/45470, etc related issues); all the existing interfaces mostly assume symlinks don't exist (fs.DirEntry.Info() and fs.WalkDir(...) as notable exceptions 🤷)
//
// if the object we're pointing at represents a symlink, this returns the (resolved) path that should be looked up instead; only relative symlinks are supported (and attempts to escape the repository with too many "../" *should* result in an error -- this is a convenience/sanity check, not a security boundary; subset of https://pkg.go.dev/io/fs#ValidPath)
//
// otherwise, it will return the empty string and nil
func (f gitFS) resolveLink() (string, error) {
	isLink, target, err := f.readLink()
	if !isLink || err != nil {
		return "", err
	}

	if target == "" {
		return "", fmt.Errorf("unexpected: empty symlink %q", f.name)
	}

	// we *could* implement this as absolute symlinks being relative to the root of the Git repository, but that wouldn't match the behavior of a normal repository that's been "git clone"'d on disk, so I think that would be a mistake and erroring out is saner here
	if path.IsAbs(target) {
		return "", fmt.Errorf("unsupported: %q is an absolute symlink (%q)", f.name, target)
	}

	// symlinks are relative to the path they're in, so we need to prepend that
	target = path.Join(path.Dir(f.name), target)

	// now let's use path.Clean to get rid of any excess ".." or "." entries in our end result
	target = path.Clean(target)

	// once we're cleaned, we should have a full path that's relative to the root of the Git repository, so if it still starts with "../", that's a problem that will error later when we try to read it, so let's error out now to bail earlier
	if strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("unsupported: %q is a relative symlink outside the tree (%q)", f.name, target)
	}

	return target, nil
}

// a helper shared between FS.Stat(...) and FS.Open(...); also the primary entrypoint to creating new gitFS objects besides gitfs.CommitHash(...)
func (f gitFS) stat(name string, followSymlinks bool) (*gitFS, error) {
	if !f.IsDir() {
		return nil, fmt.Errorf("cannot stat a child (%q) of non-directory %q", name, f.name)
	}
	if path.Join(f.name, name) == f.name { // path.Join implies path.Clean too
		// (this is to defensively special-case handling of ".", which FindEntry doesn't like)
		return &f, nil
	}
	entry, err := f.tree.FindEntry(name)
	if err != nil {
		return nil, fmt.Errorf("Tree(%q).FindEntry(%q): %w", f.name, name, err)
	}
	return f.statEntry(name, entry, followSymlinks)
}

// dual-use by gitFS.stat and ReadDir (hence "followSymlinks" -- ReadDir needs to not resolve symlinks when creating sub-FS objects)
func (f gitFS) statEntry(name string, entry *goGitPlumbingObject.TreeEntry, followSymlinks bool) (*gitFS, error) {
	if entry == nil {
		return nil, fmt.Errorf("(%q).statEntry cannot accept a nil entry; perhaps you intended .stat(%q) instead?", f.name, name)
	}

	var (
		fi  = f.clone()
		err error
	)
	fi.entry = entry
	fi.name = path.Join(fi.name, name)

	if fi.IsDir() {
		fi.tree, err = goGitPlumbingObject.GetTree(f.storer, entry.Hash) // see https://github.com/go-git/go-git/blob/v5.11.0/plumbing/object/tree.go#L103
		if err != nil {
			return nil, fmt.Errorf("Tree(%q): %w", fi.name, err)
		}
		return fi, nil
	}

	fi.size, err = f.storer.EncodedObjectSize(entry.Hash) // https://github.com/go-git/go-git/blob/v5.11.0/plumbing/object/tree.go#L92
	if err != nil {
		return nil, fmt.Errorf("Size(%q): %w", fi.name, err)
	}

	if followSymlinks {
		// TODO this should probably be an explicit loop (instead of implicit recursion) with some upper nesting limit? (symlink to symlink to symlink to ...; possibly even in an infinite cycle because symlinks)
		if target, err := fi.resolveLink(); err != nil {
			return nil, err
		} else if target != "" {
			// the value from resolveLink is relative to the root
			return f.root.stat(target, followSymlinks)
			// ideally this would "just" use "path.Rel" to make "target" relative to "f.name" instead, but "path.Rel" does not exist and only "filepath.Rel" does which would break this code on Windows, so instead we added a "root" pointer that we pass around forever that links us back to the root of our "Tree"
			// we could technically solve this by judicious use of "../" (with enough "../" to catch all the "/" in "f.name"), but it seems simpler and more obvious (and less error prone) to just pass around a pointer to the root
		}
	}

	return fi, nil
}

// https://pkg.go.dev/io/fs#FS
func (f gitFSFS) Open(name string) (fs.File, error) {
	pathErr := &fs.PathError{
		Op:   "open",
		Path: name,
	}
	if !fs.ValidPath(name) {
		pathErr.Err = fs.ErrInvalid
		return nil, pathErr
	}

	var fi *gitFS
	fi, pathErr.Err = f.stat(name, true)
	if pathErr.Err != nil {
		return nil, pathErr
	}

	if fi.IsDir() {
		fi.walker = goGitPlumbingObject.NewTreeWalker(fi.tree, false, nil)
		return fi, nil
	}

	var file *goGitPlumbingObject.File
	file, err := fi.tree.TreeEntryFile(fi.entry)
	if err != nil {
		pathErr.Err = fmt.Errorf("Tree(%q).TreeEntryFile(%q): %w", f.name, fi.name, err)
		return nil, pathErr
	}

	fi.reader, err = file.Reader()
	if err != nil {
		pathErr.Err = fmt.Errorf("File(%q).Reader(): %w", fi.name, err)
		return nil, pathErr
	}

	return fi, nil
}

// https://pkg.go.dev/io/fs#StatFS
func (f gitFSFS) Stat(name string) (fs.FileInfo, error) {
	fi, err := f.stat(name, true)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "stat",
			Path: name,
			Err:  err,
		}
	}
	return fi, nil
}

// https://github.com/golang/go/issues/49580 ("type ReadLinkFS interface")
func (f gitFSFS) ReadLink(name string) (string, error) {
	fi, err := f.stat(name, false)
	if err != nil {
		return "", &fs.PathError{
			Op:   "readlink",
			Path: name,
			Err:  err,
		}
	}
	isLink, target, err := fi.readLink()
	if err != nil {
		return "", &fs.PathError{
			Op:   "readlink",
			Path: name,
			Err:  err,
		}
	}
	if !isLink {
		return "", &fs.PathError{
			Op:   "readlink",
			Path: name,
			Err:  fmt.Errorf("not a symlink"),
		}
	}
	return target, nil
}

// https://pkg.go.dev/io/fs#SubFS
func (f gitFS) Sub(dir string) (fs.FS, error) {
	fi, err := f.stat(dir, true)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", fi.name)
	}
	return gitFSFS{gitFS: fi}, nil
}

// https://pkg.go.dev/io/fs#File
func (f gitFS) Stat() (fs.FileInfo, error) {
	return f, nil
}

// https://pkg.go.dev/io/fs#File
func (f gitFS) Read(b []byte) (int, error) {
	if f.reader == nil {
		return 0, fmt.Errorf("%q not open (or not a file)", f.name)
	}
	return f.reader.Read(b)
}

// https://pkg.go.dev/io/fs#File
func (f gitFS) Close() error {
	if f.reader != nil {
		if err := f.reader.Close(); err != nil {
			return err
		}
	}
	if f.walker != nil {
		f.walker.Close() // returns no error, nothing 🤔
	}
	return nil
}

// https://pkg.go.dev/io/fs#ReadDirFile
func (f gitFS) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.walker == nil {
		return nil, fmt.Errorf("%q not open (or not a directory)", f.name)
	}
	ret := []fs.DirEntry{}
	for i := 0; n <= 0 || i < n; i++ {
		name, entry, err := f.walker.Next()
		if err != nil {
			if err == io.EOF && n <= 0 {
				// "In this case, if ReadDir succeeds (reads all the way to the end of the directory), it returns the slice and a nil error."
				break
			}
			return ret, err
		}
		fi, err := f.statEntry(name, &entry, false)
		if err != nil {
			return ret, err
		}
		ret = append(ret, fi)
	}
	return ret, nil
}

// https://pkg.go.dev/io/fs#FileInfo: base name of the file
func (f gitFS) Name() string {
	return path.Base(f.name) // this should be the same as f.entry.Name (except in the case of the top-level / root)
}

// https://pkg.go.dev/io/fs#FileInfo: length in bytes for regular files; system-dependent for others
func (f gitFS) Size() int64 {
	return f.size
}

// https://pkg.go.dev/io/fs#FileInfo: file mode bits
func (f gitFS) Mode() fs.FileMode {
	// https://pkg.go.dev/github.com/go-git/go-git/v5@v5.4.2/plumbing/filemode#FileMode
	// https://pkg.go.dev/io/fs#FileMode
	if f.entry == nil {
		// "." at the top-level of the repository is a directory
		return 0775 | fs.ModeDir
	}
	switch f.entry.Mode {
	case goGitPlumbingFileMode.Regular:
		return 0664
	case goGitPlumbingFileMode.Symlink:
		return 0777 | fs.ModeSymlink
	case goGitPlumbingFileMode.Executable:
		return 0775
	case goGitPlumbingFileMode.Dir:
		return 0775 | fs.ModeDir
	}
	return 0 | fs.ModeIrregular // TODO what to do for files whose types we don't support? 😬
}

// https://pkg.go.dev/io/fs#FileInfo: modification time
func (f gitFS) ModTime() time.Time {
	return f.Mod
}

// https://pkg.go.dev/io/fs#FileInfo: abbreviation for Mode().IsDir()
func (f gitFS) IsDir() bool {
	return f.Mode().IsDir()
}

// https://pkg.go.dev/io/fs#FileInfo: underlying data source (can return nil)
func (f gitFS) Sys() interface{} {
	return nil
}

// https://pkg.go.dev/io/fs#DirEntry
func (f gitFS) Type() fs.FileMode {
	return f.Mode().Type()
}

// https://pkg.go.dev/io/fs#DirEntry
func (f gitFS) Info() (fs.FileInfo, error) {
	return f, nil
}
