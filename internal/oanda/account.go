package oanda

import "encoding/json"

// GetAccounts mengembalikan daftar akun yang bisa diakses token.
// Dipakai sebagai smoke-test: kalau berhasil, token valid.
func (c *Client) GetAccounts() ([]Account, error) {
	body, err := c.get("/v3/accounts", nil)
	if err != nil {
		return nil, err
	}
	var r accountsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return r.Accounts, nil
}
