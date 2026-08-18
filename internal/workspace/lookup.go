package workspace

// DocumentLookup identifies a retained document and the workspace snapshot that owns it.
type DocumentLookup struct {
	Document  Document
	Workspace ID
	Revision  uint64
}
