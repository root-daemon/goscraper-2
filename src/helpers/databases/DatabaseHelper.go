package databases

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"goscraper/src/globals"
	"io"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/supabase-community/supabase-go"
)

type DatabaseHelper struct {
	client *supabase.Client
	key    []byte
}

func NewDatabaseHelper() (*DatabaseHelper, error) {
	if globals.DevMode {
		godotenv.Load()
	}
	supabaseUrl := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")
	encryptionKey := os.Getenv("ENCRYPTION_KEY")

	client, err := supabase.NewClient(supabaseUrl, supabaseKey, nil)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(encryptionKey))
	return &DatabaseHelper{
		client: client,
		key:    hash[:],
	}, nil
}

func (db *DatabaseHelper) encrypt(text string) (string, error) {
	block, err := aes.NewCipher(db.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(text), nil)
	encrypted := base64.StdEncoding.EncodeToString(ciphertext)
	return encrypted, nil
}

func (db *DatabaseHelper) decrypt(encryptedText string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(db.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", err
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func (db *DatabaseHelper) UpsertData(table string, data map[string]interface{}) error {
	regNumber, hasRegNumber := data["regNumber"]
	token, hasToken := data["token"]

	data["lastUpdated"] = time.Now().UnixNano() / int64(time.Millisecond)

	for key, value := range data {
		if key != "regNumber" && key != "token" && key != "lastUpdated" && key != "timetable" && key != "ophour" {
			jsonBytes, err := json.Marshal(value)
			if err != nil {
				return err
			}
			encrypted, err := db.encrypt(string(jsonBytes))
			if err != nil {
				return err
			}
			data[key] = encrypted
		}

	}

	if hasRegNumber {
		data["regNumber"] = regNumber
	}
	if hasToken {
		data["token"] = token
	}

	_, _, err := db.client.From(table).Upsert(data, "regNumber", "", "").Execute()
	return err
}

func (db *DatabaseHelper) ReadData(table string, query map[string]interface{}) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	queryAsString := make(map[string]string)
	for k, v := range query {
		if str, ok := v.(string); ok {
			queryAsString[k] = str
		}
	}

	_, _, err := db.client.From(table).Select("*", "", false).Match(queryAsString).Execute()
	if err != nil {
		return nil, err
	}

	for _, row := range results {
		for key, value := range row {
			if str, ok := value.(string); ok {
				if key != "regNumber" && key != "token" && key != "lastUpdated" && key != "timetable" && key != "ophour" {
					decrypted, err := db.decrypt(str)
					if err != nil {
						return nil, err
					}
					row[key] = decrypted
				}
			}
		}
	}

	return results, nil
}

func (db *DatabaseHelper) FindByToken(table string, token string) (map[string]interface{}, error) {
	var results []map[string]interface{}

	query := map[string]string{
		"token": token,
	}

	_, err := db.client.From(table).Select("*", "", false).Match(query).ExecuteTo(&results)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	for key, value := range results[0] {
		if str, ok := value.(string); ok {
			if key == "timetable" {
				var jsonData interface{}
				if err := json.Unmarshal([]byte(str), &jsonData); err != nil {
					return nil, err
				}
				results[0][key] = jsonData
			} else if key != "regNumber" && key != "token" && key != "lastUpdated" && key != "timetable" && key != "ophour" {
				decrypted, err := db.decrypt(str)
				if err != nil {
					return nil, err
				}
				var jsonData interface{}
				if err := json.Unmarshal([]byte(decrypted), &jsonData); err != nil {
					return nil, err
				}
				results[0][key] = jsonData
			}
		}
	}

	return results[0], nil
}

type StoredCredentials struct {
	RegNumber string
	Account   string
	Password  string
	Cookies   string
}

func (db *DatabaseHelper) GetCredentialsByToken(token string) (*StoredCredentials, error) {
	var results []map[string]interface{}

	query := map[string]string{
		"token": token,
	}

	_, err := db.client.From("goscrape").Select("regNumber,account,password,cookies", "", false).Match(query).ExecuteTo(&results)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	row := results[0]
	regNumber, _ := row["regNumber"].(string)

	accountEnc, _ := row["account"].(string)
	passwordEnc, _ := row["password"].(string)
	cookiesEnc, _ := row["cookies"].(string)

	if accountEnc == "" || passwordEnc == "" {
		return nil, nil
	}

	accountDec, err := db.decrypt(accountEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt account: %w", err)
	}

	passwordDec, err := db.decrypt(passwordEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	var account, password string
	if err := json.Unmarshal([]byte(accountDec), &account); err != nil {
		account = accountDec
	}
	if err := json.Unmarshal([]byte(passwordDec), &password); err != nil {
		password = passwordDec
	}

	var cookies string
	if cookiesEnc != "" {
		cookiesDec, err := db.decrypt(cookiesEnc)
		if err == nil {
			if err := json.Unmarshal([]byte(cookiesDec), &cookies); err != nil {
				cookies = cookiesDec
			}
		}
	}

	return &StoredCredentials{
		RegNumber: regNumber,
		Account:   account,
		Password:  password,
		Cookies:   cookies,
	}, nil
}

func (db *DatabaseHelper) UpdateSession(regNumber, newToken, newCookies string) error {
	encryptedCookies, err := db.encrypt(`"` + newCookies + `"`)
	if err != nil {
		return fmt.Errorf("failed to encrypt cookies: %w", err)
	}

	data := map[string]interface{}{
		"regNumber":   regNumber,
		"token":       newToken,
		"cookies":     encryptedCookies,
		"lastUpdated": time.Now().UnixNano() / int64(time.Millisecond),
	}

	_, _, err = db.client.From("goscrape").Upsert(data, "regNumber", "", "").Execute()
	return err
}

func (db *DatabaseHelper) GetOphourByToken(token string) (string, error) {
	var results []map[string]interface{}

	query := map[string]string{
		"token": token,
	}

	_, err := db.client.From("goscrape").Select("ophour", "", false).Match(query).ExecuteTo(&results)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", nil
	}

	ophour, ok := results[0]["ophour"].(string)
	if !ok {
		return "", nil
	}
	return ophour, nil
}
