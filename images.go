package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// MediaData holds information about extracted images
type MediaData struct {
	URL        string
	ImageURLs  []string
	StatusCode int
	meta       string
}

// Sitemap structure to parse XML sitemap data
type Sitemap struct {
	XMLName xml.Name `xml:"urlset"`
	Urls    []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// Parser defines the parsing interface
type Parser interface {
	GetMediaData(resp *http.Response) (MediaData, error)
}

// DefaultParser is an empty struct for implementing the default parser
type DefaultParser struct {
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:56.0) Gecko/20100101 Firefox/56.0",
}

// randomUserAgent returns a random User-Agent string
func randomUserAgent() string {
	// Obtain a random number from the Unix Timestamp
	rand.Seed(time.Now().Unix())
	randNum := rand.Int() % len(userAgents)
	return userAgents[randNum]
}

// makeRequest sends an HTTP GET request with a random User-Agent header
func makeRequest(url string) (*http.Response, error) {

	// Creates an HTTP client with a timeout of 10 seconds for the request.
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	// HTTP Get Request for thee url given
	req, err := http.NewRequest("GET", url, nil)

	// Set the User-Agent Header to the randomly chosen agent.
	req.Header.Set("User-Agent", randomUserAgent())
	if err != nil {
		return nil, err
	}

	// Sends the HTTP get request and returns the result
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetMediaData extracts all image URLs from the response
func (d DefaultParser) GetMediaData(resp *http.Response) (MediaData, error) {

	// Creates a goquery Document from the HTTP response
	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		return MediaData{}, err
	}

	imageURLs := []string{}

	// Searches the goquery Document for img tags and the src link
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		// If the src link exists, add it to the imageURLs string list
		if exists {
			imageURLs = append(imageURLs, src)
		}
	})

	// Construct the MediaData struct with new info
	result := MediaData{
		URL:        resp.Request.URL.String(),
		ImageURLs:  imageURLs,
		StatusCode: resp.StatusCode,
	}
	result.meta, _ = doc.Find("meta[name^=description]").Attr("content")
	return result, nil
}

// parseSitemap parses the XML sitemap and returns the URLs
func parseSitemap(sitemapURL string) ([]string, error) {
	resp, err := makeRequest(sitemapURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var sitemap Sitemap
	decoder := xml.NewDecoder(resp.Body)
	err = decoder.Decode(&sitemap)
	if err != nil {
		return nil, err
	}

	var urls []string
	for _, url := range sitemap.Urls {
		urls = append(urls, url.Loc)
	}
	return urls, nil
}

// scrapeImages fetches image data from a list of URLs
func scrapeImages(urls []string, parser Parser, concurrency int) []MediaData {
	tokens := make(chan struct{}, concurrency)
	results := []MediaData{}
	worklist := make(chan string, len(urls))
	var mu sync.Mutex

	// Start scraping in parallel
	for _, url := range urls {
		go func(url string) {
			tokens <- struct{}{}        // acquire a token
			defer func() { <-tokens }() // release the token when done

			log.Printf("Scraping URL: %s", url)
			resp, err := makeRequest(url)
			if err != nil {
				log.Printf("Error requesting URL %s: %v", url, err)
				return
			}

			data, err := parser.GetMediaData(resp)
			if err != nil {
				log.Printf("Error parsing media data for URL %s: %v", url, err)
				return
			}

			mu.Lock()
			// Append result to the results slice
			results = append(results, data)
			mu.Unlock()

			// Send completion signal to the main goroutine
			worklist <- url
		}(url)
	}

	// Wait for all scraping goroutines to finish
	for range urls {
		<-worklist
	}

	return results
}

func main() {
	// Define sitemap URL
	sitemapURL := "https://www.espn.com/googlenewssitemap"

	// Create output file
	outputFile, err := os.Create("image_results.txt")
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer outputFile.Close()

	// Create a DefaultParser instance
	parser := DefaultParser{}

	// Parse the sitemap and get all the URLs
	urls, err := parseSitemap(sitemapURL)
	if err != nil {
		log.Fatalf("Error parsing sitemap: %v", err)
	}

	// Scrape the URLs for images with concurrency
	concurrency := 50 // Number of concurrent requests
	results := scrapeImages(urls, parser, concurrency)

	// Save the results to the file
	for _, res := range results {
		output := fmt.Sprintf("URL: %s\nStatusCode: %d\nMeta Description: %s\nImages:\n", res.URL, res.StatusCode, res.meta)
		for _, imgURL := range res.ImageURLs {
			output += fmt.Sprintf("- %s\n", imgURL)
		}
		output += "\n"
		_, err := outputFile.WriteString(output)
		if err != nil {
			log.Printf("Error writing to file for URL %s: %v", res.URL, err)
		}
	}

	fmt.Println("Image extraction completed. Results saved to image_results.txt")
}
