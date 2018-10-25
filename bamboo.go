package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgentry/speakeasy"
	"github.com/gocolly/colly"
	"github.com/kennygrant/sanitize"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/tidwall/gjson"
)

var (
	col       = colly.NewCollector()
	homeDir   string
	subdomain string
)

// Login - Login with credentials
func Login(user, pass string) error {
	csrfToken := ""
	loginToken := ""
	tzToken := ""
	rToken := ""

	// attach callbacks to get the hidden form fields
	col.OnHTML("input[name='CSRFToken']", func(e *colly.HTMLElement) {
		csrfToken = e.Attr("value")
	})
	col.OnHTML("input[name='login']", func(e *colly.HTMLElement) {
		loginToken = e.Attr("value")
	})
	col.OnHTML("input[name='tz']", func(e *colly.HTMLElement) {
		tzToken = e.Attr("value")
	})
	col.OnHTML("input[name='r']", func(e *colly.HTMLElement) {
		rToken = e.Attr("value")
	})

	// visit the login page (initial GET so we can populate the CSRFToken, etc...)
	col.Visit(fmt.Sprintf("https://%s.bamboohr.com/login.php", subdomain))

	// authenticate with a POST
	err := col.Post(fmt.Sprintf("https://%s.bamboohr.com/login.php", subdomain), map[string]string{
		"username":  user,
		"password":  pass,
		"login":     loginToken,
		"tz":        tzToken,
		"r":         rToken,
		"CSRFToken": csrfToken})
	if err != nil {
		return err
	}
	return nil
}

// Candidate - The details tracked for each candidate
type Candidate struct {
	ApplicantID           string `json:"applicantId"`
	Archived              string `json:"archived"`
	CoverLetterFileID     string `json:"coverLetterFileId"`
	CoverLetterFileDataID string `json:"coverLetterFileDataId"`
	CoverLetterFileName   string `json:"coverLetterFileName"`
	Email                 string `json:"email"`
	FirstName             string `json:"firstName"`
	LastName              string `json:"lastName"`
	LastUpdatedDate       string `json:"lastUpdatedDate"`
	Phone                 string `json:"phone"`
	PositionApplicantID   string `json:"positionApplicantId"`
	PositionID            string `json:"positionId"`
	Position              string
	Rating                string `json:"rating"`
	ResumeFileID          string `json:"resumeFileId"`
	ResumeFileDataID      string `json:"resumeFileDataId"`
	ResumeFileName        string `json:"resumeFileName"`
	StatusID              string `json:"statusId"`
	DateAdded             string `json:"dateAdded"`
	LinkedinURL           string `json:"linkedinUrl"`
	WebsiteURL            string `json:"websiteUrl"`
}

// DownloadResume - Download the resume file to the specified path
func (c *Candidate) DownloadResume(path string) error {
	FilePath := fmt.Sprintf("%s%s%s-%s-%s-[%s]%s",
		path,
		string(os.PathSeparator),
		sanitize.BaseName(c.FirstName),
		sanitize.BaseName(c.LastName),
		c.Rating,
		sanitize.BaseName(c.Position),
		filepath.Ext(c.ResumeFileName))
	// only download if the file does not already exist
	if _, err := os.Stat(FilePath); os.IsNotExist(err) {
		fmt.Println(FilePath)
		// NOTE -- START
		// We are not able to use `colly` as per the code below because it corrupts the saved files.

		// col.OnResponse(func(r *colly.Response) {
		// 	r.Save(FilePath)
		// })
		// // get file
		// col.Visit(fmt.Sprintf("https://%s.bamboohr.com/files/download.php?id=%s", subdomain, c.ResumeFileID))

		// NOTE -- END

		// expressive file download functionality
		url := fmt.Sprintf("https://%s.bamboohr.com/files/download.php?id=%s", subdomain, c.ResumeFileID)
		cookies := col.Cookies(url)

		// Create the file
		out, err := os.Create(FilePath)
		if err != nil {
			return err
		}
		defer out.Close()

		// Declare http client
		client := &http.Client{}

		// Build the Request
		req, err := http.NewRequest("GET", url, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Write the body to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("File exists")
}

// QueryCandidates - Get Candidates with a query string (check format via the Ajax on-page)
func QueryCandidates(query string) ([]Candidate, error) {
	candidates := make([]Candidate, 0)

	// candidates page query (json data)
	col.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	})

	// when the json data is received, populate our object
	col.OnResponse(func(r *colly.Response) {
		jsonStr := string(r.Body)
		// fmt.Println(jsonStr)

		ids := gjson.Get(jsonStr, "data.candidates.allIds")
		//fmt.Println(ids)

		for _, id := range ids.Array() {
			//fmt.Println(id)
			details := gjson.Get(jsonStr, fmt.Sprintf("data.candidates.byIds.%d", id.Int()))
			//fmt.Println(details)

			var candidate Candidate
			if err := json.Unmarshal([]byte(details.String()), &candidate); err != nil {
				fmt.Println(err.Error())
			}
			candidate.Position = gjson.Get(jsonStr, fmt.Sprintf("data.positions.byIds.%s.name", candidate.PositionID)).String()
			//fmt.Println(candidate)
			candidates = append(candidates, candidate)
		}
	})

	// do the actual query for the json data
	col.Visit(fmt.Sprintf("https://%s.bamboohr.com/hiring/candidates?%s", subdomain, query))

	return candidates, nil
}

// execution starts here...
func main() {
	homeDir, _ := homedir.Dir()
	user := flag.String("u", "", "Email Address of the user (required)")
	pass := flag.String("p", "", "Password of the user (optional)")
	limit := flag.String("n", "500", "Number of results to query (optional)")
	sd := flag.String("subdomain", "cloudops", "Subdomain in BambooHR [<subdomain>.bamboohr.com] (optional)")
	path := flag.String("dl", fmt.Sprintf("%s%sGoogle Drive File Stream%sTeam Drives%sHR Drive%sBamboo Resumes",
		homeDir, string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator)), "Path to save the files to (validate)")
	flag.Parse()
	subdomain = *sd

	// user email is required
	if *user == "" {
		log.Fatal("'-u' flag is required")
	}
	if *pass == "" {
		password, err := speakeasy.Ask("Enter your password: ")
		if err != nil {
			log.Fatal(err)
		}
		flag.Set("p", password)
	}
	// change the the filepath to be an absolute path to the location
	absPath, err := filepath.Abs(*path)
	if err != nil {
		log.Fatal(err)
	}
	flag.Set("dl", strings.TrimSuffix(absPath, string(os.PathSeparator)))

	fmt.Println("Starting downloads...")

	// User-Agent used for requests (Chrome)
	col.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36"

	// login to bamboo
	err = Login(*user, *pass)
	if err != nil {
		log.Fatal(err)
	}

	// query for candidate data
	candidates, err := QueryCandidates(fmt.Sprintf("offset=0&limit=%s&sortOrder=DESC", *limit))
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Println(candidates)

	// download resumes
	downloaded := 0
	for _, c := range candidates {
		err = c.DownloadResume(*path)
		if err == nil {
			downloaded++
		}
	}

	fmt.Println("Downloaded", downloaded, "resumes")
}
