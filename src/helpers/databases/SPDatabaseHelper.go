package databases

import (
	"encoding/json"
	"fmt"
	"time"
)

// SPDatabaseHelper persists sp.srmist.edu.in (student portal) sessions and scraped
// data in the `sp_scrape` Supabase table. It reuses DatabaseHelper's encryption
// helpers so credentials and cookies stay encrypted at rest, matching how
// goscrape handles academia data.
type SPDatabaseHelper struct {
	*DatabaseHelper
}

func NewSPDatabaseHelper() (*SPDatabaseHelper, error) {
	db, err := NewDatabaseHelper()
	if err != nil {
		return nil, err
	}
	return &SPDatabaseHelper{DatabaseHelper: db}, nil
}

type SPStoredSession struct {
	NetID     string
	Account   string
	Password  string
	Cookies   string
	Token     string
	RegNumber string
}

func (db *SPDatabaseHelper) UpsertSession(s SPStoredSession) error {
	if s.NetID == "" {
		return fmt.Errorf("sp upsert: empty netid")
	}

	accountEnc, err := db.encrypt(`"` + s.Account + `"`)
	if err != nil {
		return fmt.Errorf("encrypt account: %w", err)
	}
	passwordEnc, err := db.encrypt(`"` + s.Password + `"`)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	cookiesEnc, err := db.encrypt(`"` + s.Cookies + `"`)
	if err != nil {
		return fmt.Errorf("encrypt cookies: %w", err)
	}

	row := map[string]interface{}{
		"netid":       s.NetID,
		"account":     accountEnc,
		"password":    passwordEnc,
		"cookies":     cookiesEnc,
		"token":       s.Token,
		"regNumber":   s.RegNumber,
		"lastUpdated": time.Now().UnixNano() / int64(time.Millisecond),
	}

	_, _, err = db.client.From("sp_scrape").Upsert(row, "netid", "", "").Execute()
	return err
}

func (db *SPDatabaseHelper) FindByToken(token string) (*SPStoredSession, error) {
	var rows []map[string]interface{}
	_, err := db.client.From("sp_scrape").Select("*", "", false).
		Match(map[string]string{"token": token}).ExecuteTo(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return decodeSPRow(db.DatabaseHelper, rows[0])
}

func (db *SPDatabaseHelper) FindByNetID(netid string) (*SPStoredSession, error) {
	var rows []map[string]interface{}
	_, err := db.client.From("sp_scrape").Select("*", "", false).
		Match(map[string]string{"netid": netid}).ExecuteTo(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return decodeSPRow(db.DatabaseHelper, rows[0])
}

func (db *SPDatabaseHelper) UpdateCookies(netid, newToken, newCookies string) error {
	cookiesEnc, err := db.encrypt(`"` + newCookies + `"`)
	if err != nil {
		return fmt.Errorf("encrypt cookies: %w", err)
	}
	row := map[string]interface{}{
		"netid":       netid,
		"token":       newToken,
		"cookies":     cookiesEnc,
		"lastUpdated": time.Now().UnixNano() / int64(time.Millisecond),
	}
	_, _, err = db.client.From("sp_scrape").Upsert(row, "netid", "", "").Execute()
	return err
}

// UpsertScrapedData stores marks/attendance JSON blobs (encrypted) for a netid.
// Pass keys like "marks" or "attendance" mapping to JSON-marshalable values.
func (db *SPDatabaseHelper) UpsertScrapedData(netid string, fields map[string]interface{}) error {
	if netid == "" {
		return fmt.Errorf("sp upsert scraped: empty netid")
	}
	row := map[string]interface{}{
		"netid":       netid,
		"lastUpdated": time.Now().UnixNano() / int64(time.Millisecond),
	}
	for k, v := range fields {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", k, err)
		}
		enc, err := db.encrypt(string(raw))
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", k, err)
		}
		row[k] = enc
	}
	_, _, err := db.client.From("sp_scrape").Upsert(row, "netid", "", "").Execute()
	return err
}

func decodeSPRow(db *DatabaseHelper, row map[string]interface{}) (*SPStoredSession, error) {
	netid, _ := row["netid"].(string)
	regNumber, _ := row["regNumber"].(string)
	token, _ := row["token"].(string)

	account, err := decryptStringField(db, row["account"])
	if err != nil {
		return nil, fmt.Errorf("decrypt account: %w", err)
	}
	password, err := decryptStringField(db, row["password"])
	if err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}
	cookies, err := decryptStringField(db, row["cookies"])
	if err != nil {
		return nil, fmt.Errorf("decrypt cookies: %w", err)
	}

	return &SPStoredSession{
		NetID:     netid,
		Account:   account,
		Password:  password,
		Cookies:   cookies,
		Token:     token,
		RegNumber: regNumber,
	}, nil
}

func decryptStringField(db *DatabaseHelper, raw interface{}) (string, error) {
	enc, _ := raw.(string)
	if enc == "" {
		return "", nil
	}
	dec, err := db.decrypt(enc)
	if err != nil {
		return "", err
	}
	var v string
	if err := json.Unmarshal([]byte(dec), &v); err == nil {
		return v, nil
	}
	return dec, nil
}
