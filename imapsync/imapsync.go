package main

/**
 * Some helpfull links:
 * https://github.com/emersion/go-imap/wiki/Fetching-messages
 * https://github.com/emersion/go-imap
 * https://github.com/golang/go/wiki/SQLInterface
 * https://github.com/lib/pq/issues/678
 * https://godoc.org/github.com/lib/pq
 *
 */

import (
	"log"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap"
	_ "github.com/lib/pq"
	"database/sql"
	"io"
	"io/ioutil"
	"time"
	"fmt"
	"gopkg.in/yaml.v2"
	"unicode/utf8"
	"strings"
	"bytes"
	"github.com/emersion/go-message"
)

const ConfigFile = "./config.yml"

type Config struct {
	Imap struct {
		Username 	string `yaml:"username"`
		Password 	string `yaml:"password"`
	}
	Database struct {
		Name 		string `yaml:"name"`
		Username 	string `yaml:"username"`
		Password 	string `yaml:"password"`
	}
}

type Email struct {
	Subject 	string `json:"subject"`
	Received 	time.Time `json:"received"`
	From 		string `json:"from"`
	To 			string `json:"to"`
	Body 		string `json:"body"`
}

var db *sql.DB

func main() {

	log.Printf("Reading configuration from: \"%s\"", ConfigFile)
	config := loadConfig()

	log.Println("Opening database connection...")

	var err error
	connStr := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=disable", config.Database.Username, config.Database.Password, config.Database.Name)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	//db.SetConnMaxLifetime(time.Second * 10)

	log.Println("Connecting to imap server...")

	// Connect to server
	c, err := client.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Connected")

	// Don't forget to logout
	defer c.Logout()

	// Login
	if err := c.Login(config.Imap.Username, config.Imap.Password); err != nil {
		log.Fatal(err)
	}
	log.Println("Logged in")

	// List mailboxes
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	log.Println("Mailboxes:")
	for m := range mailboxes {
		log.Println("* " + m.Name)
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}

	// Select the ALL mailbox
	mbox, err := c.Select("[Gmail]/All Mail", true)
	if err != nil {
		log.Fatal(err)
	}

	amount := uint32(10000)
	log.Printf("[Gmail]/All Mail has %d messages", mbox.Messages)
	log.Printf("Fetching %d messages from all.. ", amount)
	fetchMessages(c, mbox, amount)

	defer db.Close()
	log.Println("Done!")
}

func loadConfig() (Config) {

	config := Config{}
	contents, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		log.Fatalf("Error loading config file: %s\n", err)
	}

	err = yaml.Unmarshal(contents, &config)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	return config
}

func fetchMessages(imapClient *client.Client, mbox *imap.MailboxStatus, amount uint32) {

	seqset := new(imap.SeqSet)
	allFrom := uint32(1)
	if mbox.Messages > amount {
		allFrom = mbox.Messages - amount
	}
	seqset.AddRange(allFrom, mbox.Messages)

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}

	go func() {
		done <- imapClient.Fetch(seqset, items, messages)
	}()

	var count int64 = 0
	for msg := range messages {

		if count % 100 == 0 {
			log.Printf("Processed %d messages.", count)
		}

		r := msg.GetBody(section)
		if r == nil {
			log.Fatal("Server did not return msg body")
		}

		mr, err := mail.CreateReader(r)
		if message.IsUnknownEncoding(err) {
			log.Printf("Error reading message. %s", err)
			continue
		} else if err != nil {
			log.Fatalf("Error reading msg: %d. %s", msg.Uid, err)
		}

		header := mr.Header
		email := headerToEmail(header)
		emailId := insertEmail(db, email)

		for {

			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				log.Printf("Error reading next part. %s", err)
				continue
			}

			switch h := p.Header.(type) {
			case mail.TextHeader:

				// This is the message's text (can be plain-text or HTML)
				data, _ := ioutil.ReadAll(p.Body)
				if bytes.ContainsAny(data, "\x00") {
					data = bytes.Replace(data, []byte("\x00"), []byte(""), -1)
				}

				body := fmt.Sprintf("%s", string(data))
				if len(strings.TrimSpace(body)) > 0 {
					if utf8.ValidString(body) {
						updateEmailWithText(db, emailId, body)
					} else {
						log.Printf("Nonvalid UTF-8 content from %s with subject: %s", email.From, email.Subject)
						break
						//log.Printf("body: \n%s", body)
					}
				} else {
					log.Printf("Empty email body from %s with subject: %s", email.From, email.Subject)
				}

			case mail.AttachmentHeader:
				// This is an attachment
				filename, _ := h.Filename()
				data, err := ioutil.ReadAll(p.Body)
				if err != nil {
					log.Printf("Error reading attachment %s", err)
				}
				insertAttachment(db, filename, data, emailId)
			}
		}
		count++
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}
}

func updateEmailWithText(db *sql.DB, emailId int64, text string) {
	var updatedId int64
	sql := "UPDATE EMAIL SET content = $1 WHERE id = $2 RETURNING id"
	err := db.QueryRow(sql, text, emailId).Scan(&updatedId)
	if err != nil {
		log.Fatalf("Error updating content for email with id: %d, %s", emailId, err)
	}
}

func headerToEmail(header mail.Header) (email Email) {
	var rs Email

	if date, err := header.Date(); err == nil {
		rs.Received = date
	}

	if subject, err := header.Subject(); err == nil {
		rs.Subject = subject
	}

	if from, err := header.AddressList("From"); err == nil && len(from) > 0 {
		if len(from) == 0 {
			log.Fatal("no from!?!?")
		} else {
			rs.From = fmt.Sprintf("%s", from[0])
		}
	}

	if to, err := header.AddressList("To"); err == nil {
		if len(to) == 0 {
			log.Printf("Missing To: header. Subject: %s", rs.Subject)
		} else {
			rs.To = fmt.Sprintf("%s", to[0])
		}
	}
	return rs
}

func insertEmail(db *sql.DB, email Email) (int64) {
	var id int64
	sql := "INSERT INTO EMAIL (subject, received, mailfrom, mailto, content) VALUES ($1, $2, $3, $4, $5) RETURNING id"
	err := db.QueryRow(sql, email.Subject, email.Received, email.From, email.To, email.Body).Scan(&id)
	if err != nil {
		log.Fatalf("Error inserting email from: %s with subject: %s. %s", email.From, email.Subject, err)
	}
	return id
}

func insertAttachment(db *sql.DB, filename string, bytes []byte, emailId int64) {
	//ioutil.WriteFile("/tmp/"+filename, bytes, 0644)
	_, err := db.Exec("INSERT INTO attachment (filename, data, email) VALUES ($1, $2, $3)", filename, bytes, emailId)
	if err != nil {
		log.Fatalf("Error inserting attachment: %s. %s", filename, err)
	}
}
