package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HTTPResponse struct {
	Status        int
	Date          string
	ContentType   string
	ContentLength int
	Connection    string
	SetCookie     string
	cfrequestid   string
	ExpectCT      string
	ReportTo      string
	NEL           string
	Server        string
	CFRay         string
	Body          string
}

type Profile struct {
	StatusCode int
	Time       int
	Size       int
}

type Diagnostic struct {
	RequestCount     int
	FastestTime      int
	SlowestTime      int
	MeanTime         float32
	MedianTime       float32
	PercentSuccess   float32
	ErrorCodes       []int
	SmallestResponse int
	LargestResponse  int
}

// request takes in a url struct and returns, if successful,
// a byte array of what is read from the tcp connection, and a Duration
func request(url *url.URL) (string, time.Duration) {
	startTime := time.Now() // current local time
	timeout, _ := time.ParseDuration("5s")

	var d net.Dialer
	d.Timeout = timeout
	conn, err := tls.DialWithDialer(&d, "tcp", url.Host+":https", nil)
	if err != nil {
		fmt.Println("Connection error: " + err.Error())
		fmt.Println(conn)
		os.Exit(0)
	}
	defer conn.Close() // close connection

	_, err = conn.Write([]byte("GET " + url.Path + " HTTP/1.0\r\n" + "Host: " + url.Host + "\r\n\r\n"))
	if err != nil {
		fmt.Println("Write error: " + err.Error())
		os.Exit(0)
	}

	response, err := ioutil.ReadAll(conn)
	if err != nil {
		fmt.Println("Problem with read: " + err.Error())
		os.Exit(0)
	}

	return string(response[:]), time.Since(startTime)
}

func handleResponse(response_str string) HTTPResponse {
	//response_str := string(resp[:])
	var response HTTPResponse
	// populate HTTPResponse with fields
	bodyStart := false
	for _, line := range strings.Split(response_str, "\r\n") {
		line_split := strings.Split(line, ":")
		if bodyStart || len(line) == 0 {
			bodyStart = true
			response.Body += line[:]
			continue
		}
		if len(line_split) == 1 {
			if strings.Contains(line_split[0], "HTTP") {
				response.Status, _ = strconv.Atoi(line_split[0][9:12])
				continue
			}
		}
		response_type := line_split[0]
		response_field := line_split[1]
		if response_type == "Date" {
			response.Date = response_field
		} else if response_type == "ContentType" {
			response.ContentType = response_field
		} else if response_type == "Content-Length" {
			response.ContentLength, _ = strconv.Atoi(response_field)
		} else if response_type == "Connection" {
			response.Connection = response_field
		} else if response_type == "Set-Cookie" {
			response.SetCookie = response_field
		} else if response_type == "cf-request-id" {
			response.cfrequestid = response_field
		} else if response_type == "Expect-CT" {
			response.ExpectCT = response_field
		} else if response_type == "Report-To" {
			response.ReportTo = response_field
		} else if response_type == "NEL" {
			response.NEL = response_field
		} else if response_type == "Server" {
			response.Server = response_field
		} else if response_type == "CF-Ray" {
			response.CFRay = response_field
		}
	}
	return response
}

func parseURL(rawURL string) *url.URL {
	url, err := url.ParseRequestURI(rawURL)
	if err != nil {
		panic(err)
	}
	return url
}

func verifyRequestCount(reqCount int) bool {
	if reqCount < 0 {
		return false
	}
	return false
}

func asyncRequest(url *url.URL, channel chan Profile) {
	resp, elapsedTime := request(url)
	formatted_response := handleResponse(resp)

	channel <- Profile{formatted_response.Status, int(elapsedTime.Milliseconds()), len(resp)}
}

func profile(url *url.URL, requestCount int) {
	respTimes := make([]int, 0)
	respStatusCodes := make([]int, 0)
	respByteSize := make([]int, 0)
	totalTime := 0

	channel := make(chan Profile)
	for i := 0; i < requestCount; i++ {
		go asyncRequest(url, channel)
	}
	prof := Profile{}
	for i := 0; i < requestCount; i++ {
		prof = <-channel
		respTimes = append(respTimes, prof.Time)
		respStatusCodes = append(respStatusCodes, prof.StatusCode)
		respByteSize = append(respByteSize, prof.Size)
		totalTime += prof.Time
	}
	printProfile(calculateStats(requestCount, respTimes, totalTime, respStatusCodes, respByteSize))
}

func calculateStats(requestCount int, respTimes []int, totalTime int, respStatusCodes []int, respByteSize []int) Diagnostic {
	if len(respTimes) <= 1 { // ensure profiled more than once
		fmt.Println("Didn't profile enough things! Something went wrong")
		os.Exit(1)
	}

	// calculate time diagnostics
	var slowestTime int
	var fastestTime int
	var meanTime float32
	var medianTime float32
	sort.Ints(respTimes)

	slowestTime = respTimes[len(respTimes)-1]
	fastestTime = respTimes[0]
	meanTime = float32(totalTime) / float32(requestCount)

	midPoint := len(respTimes) / 2
	if len(respTimes)%2 == 1 {
		medianTime = float32(respTimes[midPoint])
	} else {
		medianTime = float32(respTimes[midPoint-1]+respTimes[midPoint]) / 2
	}

	// success/failure diagnostics
	var failueCodes []int
	var percentSuccess float32
	for _, status := range respStatusCodes {
		if (200 <= status) && (status <= 299) { // if success code
			continue
		} else {
			failueCodes = append(failueCodes, status)
		}
	}
	percentSuccess = ((float32(requestCount) - float32(len(failueCodes))) / float32(requestCount)) * 100.0

	// response size diagnostics
	smallestResponse := math.MaxInt64
	largestResponse := -1

	for _, respSize := range respByteSize {
		if respSize < smallestResponse {
			smallestResponse = respSize
		}
		if respSize > largestResponse {
			largestResponse = respSize
		}
	}

	return Diagnostic{requestCount, fastestTime, slowestTime, meanTime, medianTime, percentSuccess, failueCodes, smallestResponse, largestResponse}
}

func printProfile(diagnostic Diagnostic) {
	fmt.Println("------------Profile Results-------------")
	fmt.Printf("Number of requests: %d\n", diagnostic.RequestCount)
	fmt.Printf("Fastest time: %d ms\n", diagnostic.FastestTime)
	fmt.Printf("Slowest time: %d ms\n", diagnostic.SlowestTime)
	fmt.Printf("Mean time: %.4g ms\n", diagnostic.MeanTime)
	fmt.Printf("Median time: %.4g ms\n", diagnostic.MedianTime)
	fmt.Printf("Percent Success: %.1f%%\n", diagnostic.PercentSuccess)
	fmt.Printf("Error codes: %v\n", diagnostic.ErrorCodes)
	fmt.Printf("Smallest response: %d bytes\n", diagnostic.SmallestResponse)
	fmt.Printf("Largest response: %d bytes\n", diagnostic.LargestResponse)
	fmt.Println("----------------------------------------")
}

func main() {
	urlPtr := flag.String("url", "https://my-worker.derekli2.workers.dev/links", "The URL to be tested")
	profilePtr := flag.Int("profile", 1, "Number of requests to be made")

	flag.Parse()

	url := parseURL(*urlPtr)

	if *profilePtr < 0 {
		fmt.Println("Not a valid number of requests")
	}

	if *profilePtr > 1 {
		profile(url, *profilePtr)
	} else {
		resp, _ := request(url)
		formatted_response := handleResponse(resp)
		fmt.Println(formatted_response.Body)
	}
}
