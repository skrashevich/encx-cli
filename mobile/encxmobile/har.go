package encxmobile

// SetHARRecordingEnabled toggles HAR 1.2 capture for Encounter HTTP traffic.
func (c *EncClient) SetHARRecordingEnabled(enabled bool) {
	c.client.SetHARRecordingEnabled(enabled)
}

// ClearHAR removes all captured HAR entries.
func (c *EncClient) ClearHAR() {
	c.client.ClearHAR()
}

// HAREntryCount returns the number of captured HAR entries.
func (c *EncClient) HAREntryCount() int64 {
	return int64(c.client.HAREntryCount())
}

// ExportHAR returns captured traffic as a HAR 1.2 JSON document.
func (c *EncClient) ExportHAR() (string, error) {
	return c.client.ExportHARJSON()
}

// HARSnapshot pairs an exported HAR document with the number of entries it contains.
type HARSnapshot struct {
	JSON       string
	EntryCount int64
}

// ExportHARSnapshot atomically exports captured traffic together with its
// entry count; pass the count to ClearHARFirst after a successful upload so
// entries captured during the upload are preserved.
func (c *EncClient) ExportHARSnapshot() (*HARSnapshot, error) {
	doc, count, err := c.client.ExportHARSnapshot()
	if err != nil {
		return nil, err
	}
	return &HARSnapshot{JSON: doc, EntryCount: int64(count)}, nil
}

// ClearHARFirst removes the n oldest captured HAR entries.
func (c *EncClient) ClearHARFirst(n int64) {
	c.client.ClearHARFirst(int(n))
}
