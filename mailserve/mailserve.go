package main

import (
	"net/http"
	"fmt"
	"encoding/json"
	"io/ioutil"
	"log"
	_ "github.com/lib/pq"
	"database/sql"
	"gopkg.in/yaml.v2"
	""
)

type Search struct {
	Query   string `json:"query"`
}

type DatabaseConfig struct {
	Name 		string `yaml:"name"`
	Username 	string `yaml:"username"`
	Password 	string `yaml:"password"`
}

const DatabaseConfigFile = "./serve-config.yml"

var db *sql.DB

func main() {

	config := loadDatabaseConfig()

	var err error
	connStr := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=disable", config.Username, config.Password, config.Name)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)

	queryEmails()

	http.HandleFunc("/service/", serviceHandler())
	log.Println("Running mail server on localhost")
	http.ListenAndServe(":8880", nil)
}

func loadDatabaseConfig() (DatabaseConfig) {

	config := DatabaseConfig{}
	contents, err := ioutil.ReadFile(DatabaseConfigFile)
	if err != nil {
		log.Fatalf("Error loading config file: %s\n", err)
	}

	err = yaml.Unmarshal(contents, &config)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	return config
}


func serviceHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		servicePath := r.URL.Path[len("/rservice/"):len(r.URL.Path)]

		//log.Debugf("request method: %s\n", r.Method)
		if r.Method == "OPTIONS" {
			w.Header().Add("Allow", "OPTIONS, GET, POST")
			return
		}

		switch servicePath {
		case "ping":
			fmt.Fprint(w, "{ pong }")
			break

		case "list":

			header := Search{}
			readBodyAndUnmarshal(w, r, &header)
			fmt.Fprint(w, `{ "ok": "true" }`)
			break

		default:
			w.WriteHeader(http.StatusNotImplemented)
			fmt.Fprint(w, "{ \"error\": \"unknown request\" }")
		}
	}
}

func queryEmails() {

	sql := "SELECT id, subject, received, mailfrom, mailto from EMAIL limit 20"
	//param := "123"
	rows, err := db.Query(sql)
	if err != nil {
		log.Printf("Error querying database, %s", err)
		return
	}

	defer rows.Close()

	for rows.Next() {
		var email mailserve.Email
		err = rows.Scan(&email)
		log.Printf("subject: %s", email.Subject)
	}
}

func readBodyAndUnmarshal(w http.ResponseWriter, r *http.Request, obj interface{}) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		http.Error(w, "Can't read body", http.StatusBadRequest)
	}
	err = json.Unmarshal(body, &obj)
	if err != nil {
		fmt.Fprintf(w, "Error parsing: %s because: %s\n", obj, err)
	}
}
