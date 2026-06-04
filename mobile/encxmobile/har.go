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
