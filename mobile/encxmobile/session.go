package encxmobile

// ExportCookies serializes session cookies to JSON bytes for persistent storage.
func (c *EncClient) ExportCookies() ([]byte, error) {
	return c.client.ExportCookies()
}

// ImportCookies restores session cookies from JSON bytes produced by ExportCookies.
func (c *EncClient) ImportCookies(data []byte) error {
	return c.client.ImportCookies(data)
}
