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
	"github.com/tidwall/gjson"
)

var (
	col = colly.NewCollector()
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

	// actually visit the login page (initial GET)
	col.Visit("https://cloudops.bamboohr.com/login.php?r=%2Fhome%2F")

	// authenticate with a POST
	err := col.Post("https://cloudops.bamboohr.com/login.php?r=%2Fhome%2F", map[string]string{
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
	FilePath := fmt.Sprintf("%s%s%s", path, string(os.PathSeparator), c.ResumeFileName)
	// only download if the file does not already exist
	if _, err := os.Stat(FilePath); os.IsNotExist(err) {
		// NOTE START
		// Not able to use `colly` as per the code below because it corrupts the saved files.

		// col.OnResponse(func(r *colly.Response) {
		// 	r.Save(FilePath)
		// })
		// // get file
		// col.Visit(fmt.Sprintf("https://cloudops.bamboohr.com/files/download.php?id=%s", c.ResumeFileID))

		// NOTE END

		url := fmt.Sprintf("https://cloudops.bamboohr.com/files/download.php?id=%s", c.ResumeFileID)
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

// QueryCandidates - Get Candidates with a query string
func QueryCandidates(query string) ([]Candidate, error) {
	candidates := make([]Candidate, 0)

	// candidates page query (json data)
	col.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	})

	col.OnResponse(func(r *colly.Response) {
		jsonStr := string(r.Body)

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
			//fmt.Println(candidate)
			candidates = append(candidates, candidate)
		}
	})

	// start scraping
	col.Visit(fmt.Sprintf("https://cloudops.bamboohr.com/hiring/candidates?%s", query))

	return candidates, nil
}

func main() {
	user := flag.String("u", "", "user's email address")
	pass := flag.String("p", "", "user's password")
	path := flag.String("dl", "~/Google Drive File Stream/Team Drives/HR Drive/Bamboo Resumes", "resume download base path")
	flag.Parse()

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

	// User-Agent used for screen scraping (Chrome)
	col.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36"

	err = Login(*user, *pass)
	if err != nil {
		log.Fatal(err)
	}

	candidates, err := QueryCandidates("offset=0&limit=500&sortOrder=DESC")
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Println(candidates)

	downloaded := 0
	for _, c := range candidates {
		err = c.DownloadResume(*path)
		if err == nil {
			downloaded++
		}
	}

	fmt.Println("Downloaded", downloaded, "resumes")
}
