package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmail "google.golang.org/api/gmail/v1"

	"github.com/gorilla/mux"
)

//Page struct for pages
type Page struct {
	URL  string
	Logo string
	Name string
	View string
	N    Notifications
	User User
}

//EsPage struct for email pages
type EsPage struct {
	URL       string
	Logo      string
	Name      string
	View      string
	N         Notifications
	User      User
	Stats     GStats
	Count     int
	Paggining GPagging
	Label     string
	Search    string
	Labels    []string
	Emails    []Thread
}

//EPage struct for email pages
type EPage struct {
	URL      string
	Logo     string
	Name     string
	View     string
	N        Notifications
	User     User
	Labels   []string
	Thread   Thread
	Messages []ThreadMessage
}

// GPagging stats
type GPagging struct {
	MinCount     int
	MaxCount     int
	NextPage     int
	PreviousPage int
}

// MailController handle other requests
var MailController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	redirect := CheckAuth(w, r, false, "/login")

	if !redirect {

		vars := mux.Vars(r)

		user := GetUser(CookieValid(r))
		labels := GetLabels(user)

		thread := GetThread(vars["treadID"], user.Email)
		messages := GetThreadMessages(user, vars["treadID"])

		p := EPage{
			Name:     "Email",
			View:     "email",
			URL:      os.Getenv("URL"),
			User:     user,
			Labels:   labels,
			Thread:   thread,
			Messages: messages,
		}

		parsedTemplate, err := template.ParseFiles(
			"template/index.html",
			"template/views/"+p.View+".html",
		)

		if err != nil {
			log.Println("Error ParseFiles:", err)
			return
		}

		err = parsedTemplate.Execute(w, p)

		if err != nil {
			log.Println("Error Execute:", err)
			return
		}

	}

})

// MailsController handle other requests
var MailsController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	redirect := CheckAuth(w, r, false, "/login")

	if !redirect {

		user := GetUser(CookieValid(r))
		stats := GetGMailsStats(user)
		labels := GetLabels(user)

		search := r.FormValue("search")

		label := ""
		label = r.FormValue("label")
		if label == "" {

			if len(labels) != 0 && search == "" {
				label = labels[0]
			}

		}

		page := r.FormValue("page")

		if page == "" {
			page = "0"
		}

		pg, _ := strconv.Atoi(page)

		gp := GPagging{
			MinCount:     pg * 50,
			MaxCount:     (pg * 50) + 50,
			NextPage:     (pg + 1),
			PreviousPage: (pg - 1),
		}

		gcount, emails := GetThreads(user, label, search, pg)

		p := EsPage{
			Name:      "Emails",
			View:      "emails",
			URL:       os.Getenv("URL"),
			User:      user,
			Search:    search,
			Label:     label,
			Labels:    labels,
			Emails:    emails,
			Count:     gcount,
			Paggining: gp,
			Stats:     stats,
		}

		parsedTemplate, err := template.ParseFiles(
			"template/index.html",
			"template/views/"+p.View+".html",
		)

		if err != nil {
			log.Println("Error ParseFiles:", err)
			return
		}

		err = parsedTemplate.Execute(w, p)

		if err != nil {
			log.Println("Error Execute:", err)
			return
		}

	}

})

// SyncController handle token requests
var SyncController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	redirect := CheckAuth(w, r, false, "/login")

	if !redirect {

		// URL vars
		vars := mux.Vars(r)

		email := vars["email"]

		u := GetUserByEmail(email)

		if r.Method == "POST" {

			query := r.FormValue("query")

			s := Syncer{
				Owner: u.Email,
				Query: query,
				Start: time.Now(),
			}

			go BackupGMail(s)

			http.Redirect(w, r, os.Getenv("URL")+"/emails", 301)
		}

		p := Page{
			Name: "Sync",
			View: "sync",
			URL:  os.Getenv("URL"),
			User: u,
		}

		parsedTemplate, err := template.ParseFiles(
			"template/index.html",
			"template/views/"+p.View+".html",
		)

		if err != nil {
			log.Println("Error ParseFiles:", err)
			return
		}

		err = parsedTemplate.Execute(w, p)

		if err != nil {
			log.Println("Error Execute:", err)
			return
		}

	}

})

// AttachController get attachment & push to client on download
var AttachController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	redirect := CheckAuth(w, r, false, "/login")

	if !redirect {

		// URL vars
		vars := mux.Vars(r)

		attachID := vars["attachID"]

		a := GetAttachment(attachID)

		for key, val := range a.Headers {
			w.Header().Set(key, val)
		}

		w.Header().Set("Expires", "0")
		w.Header().Set("Content-Length", strconv.Itoa(int(a.Size)))

		if a.Data == "gridFS" {

			gridFile := GetAttachmentGridFS(a)

			defer gridFile.Close()

			fileHeader := make([]byte, 1024)
			gridFile.Read(fileHeader)

			gridFile.Seek(0, 0)
			io.Copy(w, gridFile)

			//http.ServeContent(w, r, attach.Filename, time.Now(), gridFile) // Use proper last mod time

		} else {

			decoded, err := base64.URLEncoding.DecodeString(a.Data)
			if err != nil {
				log.Fatalf("Unable to decode attachment: %v", err)
			}
			http.ServeContent(w, r, a.Filename, time.Now(), bytes.NewReader(decoded))

		}

	}

})

// TokenController handle token requests
var TokenController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	redirect := CheckAuth(w, r, false, "/register")

	if !redirect {

		// URL vars
		vars := mux.Vars(r)

		email := vars["email"]
		code := r.FormValue("code")

		u := GetUserByEmail(email)

		tok, err := u.Config.Exchange(context.TODO(), code)
		if err != nil {
			log.Fatalf("Unable to retrieve token from web: %v", err)
		}

		u.Token = tok

		UpdateUser(u.ID.Hex(), u)

		http.Redirect(w, r, os.Getenv("URL")+"/sync/"+u.Email, 301)

	}

})

// AuthController handle other requests
var AuthController = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	uri := r.RequestURI

	var p Page

	switch uri {
	case "/":

		AppRedirect(w, r, "/login", 302)
		return

		break
	case "/logout":

		RemoveAllSessions(w)
		AppRedirect(w, r, "/login", 302)

		break
	case "/login":
		p = Page{Name: "Login", View: "login"}

		redirect := CheckAuth(w, r, true, "/emails")

		if !redirect {

			if r.Method == "POST" {

				email := r.FormValue("email")
				password := r.FormValue("password")

				if email != "" && password != "" {

					uid := CheckUser(email, password)

					if uid != "" {

						SetSession(uid, w)

						AppRedirect(w, r, "/emails", 302)
						return
					}

					AddNotification("Login", "User not valid", "error", &N)
					AppRedirect(w, r, "/login", 302)
					return
				}

				AddNotification("Login", "Please fill required fields", "error", &N)
				AppRedirect(w, r, "/login", 302)
				return
			}

			if r.Method == "GET" {

				RemoveAllSessions(w)

				p.N = N

			}
		}

		break
	case "/register":

		redirect := CheckAuth(w, r, true, "/emails")

		if !redirect {

			p = Page{Name: "Register", View: "register"}

			if r.Method == "POST" {

				// Parse our multipart form, 10 << 20 specifies a maximum
				// upload of 10 MB files.
				r.ParseMultipartForm(10 << 20)
				// FormFile returns the first file for the given key `myFile`
				// it also returns the FileHeader so we can get the Filename,
				// the Header and the size of the file
				file, _, err := r.FormFile("credentials")
				if err != nil {
					fmt.Println("Error Retrieving the File")
					fmt.Println(err)
					return
				}
				defer file.Close()
				/*
					fmt.Printf("Uploaded File: %+v\n", handler.Filename)
					fmt.Printf("File Size: %+v\n", handler.Size)
					fmt.Printf("MIME Header: %+v\n", handler.Header)
				*/
				// read all of the contents of our uploaded file into a
				// byte array
				fileBytes, err := ioutil.ReadAll(file)
				if err != nil {
					fmt.Println(err)
				}

				u := User{
					Email:       r.FormValue("email"),
					Password:    HashAndSalt(r.FormValue("password")),
					Credentials: fileBytes,
				}

				// If modifying these scopes, delete your previously saved token.json.
				config, err := google.ConfigFromJSON(u.Credentials, gmail.GmailReadonlyScope)
				if err != nil {
					log.Fatalf("Unable to parse client secret file to config: %v", err)
				}

				config.RedirectURL = os.Getenv("URL") + "token/" + u.Email + "/"

				u.Config = config

				checkUser := GetUserByEmail(u.Email)
				if checkUser.ID.Hex() == "" {

					CreateUser(u)
					user := GetUserByEmail(u.Email)

					SetSession(user.ID.Hex(), w)

					authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

					http.Redirect(w, r, authURL, 301)

				} else {

					AddNotification("Login", "User already exist", "error", &N)
					AppRedirect(w, r, "/login", 302)

				}

			}

		}

		break

	}

	p.URL = os.Getenv("URL")

	parsedTemplate, err := template.ParseFiles(
		"template/index.html",
		"template/views/"+p.View+".html",
	)

	if err != nil {
		log.Println("Error ParseFiles:", err)
		return
	}

	err = parsedTemplate.Execute(w, p)

	ClearNotification(&N)

	if err != nil {
		log.Println("Error Execute:", err)
		return
	}

})
