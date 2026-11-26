package fs

// DirectoryWriter creates new files in one pinned directory. It writes each
// file once, without a temporary file and without flushing it: it is meant
// for a private, single-use tree that the caller makes durable as a whole
// with SyncTree before anything relies on it. A file that already exists is
// never replaced. Methods may be called from several goroutines.
type DirectoryWriter struct {
	path string
	impl directoryWriterImpl
}

// Path is the directory the writer creates files in.
func (w *DirectoryWriter) Path() string { return w.path }

// Close releases the directory handle.
func (w *DirectoryWriter) Close() error {
	if w == nil || w.impl == nil {
		return nil
	}
	err := w.impl.close()
	w.impl = nil
	return err
}
