//	CTO ECAL Business Logic Helpers
//	Ed Shnekendorf, 2019, https://github.com/eshneken/cto-ecal-bizlogic

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"github.com/tidwall/gjson"
)

// Config holds all config data loaded from local config.json file
type Config struct {
	VBCSUsername    string
	VBCSPassword    string
	ECALBaseURL     string
	ServiceUsername string
	ServicePassword string
}

// Account is the type of the output for getAccounts
type Account struct {
	AccountID        string
	AccountName      string
	LOB              string
	SolutionEngineer string
	NumOpportunities string
}

// GlobalConfig is a global holder for configuration information
var GlobalConfig Config

// LineOfBusinessMapping maps LOB types to descriptions
var LineOfBusinessMapping map[string]string

func main() {
	// read system configuration from config file
	GlobalConfig = loadConfig("config.json")

	// load LOB mappings
	println("Loading LOB mappings")
	LineOfBusinessMapping = make(map[string]string)
	err := loadLinesOfBusiness()
	if err != nil {
		println(err)
		return
	}

	// register function listeners
	println("Registering REST handlers")
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/getAccounts", basicAuth(getAccountHandler))

	// start HTTP listener
	println("Starting HTTP Listener...")
	println("Connecting to VBCS Endpoint: " + GlobalConfig.ECALBaseURL)
	http.ListenAndServe(":80", nil)
}

//
// HTTP handler for health checks
//
func healthHandler(w http.ResponseWriter, r *http.Request) {
	// assume that if the service is running and successfully loaded config and LOB mappings
	// then return okay health.  In the future could be modified to poll the downstream service live
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(200)
	fmt.Fprintf(w, "HEALTH_OK")
}

//
// HTTP handler for the getAccountList functionality
//
func getAccountHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	email := query.Get("email")

	// if user
	var isManager bool
	if query.Get("isManager") == "true" {
		isManager = true
	} else {
		isManager = false
	}

	// call the helper which does the data mashing
	json, err := getAccountList(email, isManager)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, string(err.Error()))
	}

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
}

//
// Get all accounts associated with a user whether they are a manager or an individual ECA.  Function takes two parameters:
// the email of the user and a boolean indicating if they are a manager or not.  Based on this, query is constructed to return
// a list of all unique permissioned accounts for that user if they are an individual ECA or for all the users in the manager's
// org if they are a manager.  This function should return all the data needed to build the account list page.
//
func getAccountList(userEmail string, isManager bool) (string, error) {
	client := &http.Client{}

	// build the querystring
	queryString := "?fields=id,userEmail;userAccountCollection.accountObject:id,accountName,accountLOB,createdBy;userAccountCollection.accountObject.opportunityCollection:id,opportunityID&limit=5000&onlyData=true"
	if isManager {
		queryString += "&q=manager='" + userEmail + "'"
	} else {
		queryString += "&q=userEmail='" + userEmail + "'"
	}

	// call the VBCS User BO endpoint
	req, _ := http.NewRequest("GET", GlobalConfig.ECALBaseURL+"User1"+queryString, nil)
	req.SetBasicAuth(GlobalConfig.VBCSUsername, GlobalConfig.VBCSPassword)
	res, err := client.Do(req)
	if err != nil || res == nil || res.StatusCode != 200 {
		return "", errors.New(outputHTTPError("Get Accounts for email ["+userEmail+"] and isManager="+strconv.FormatBool(isManager), err, res))
	}
	defer res.Body.Close()
	j, _ := ioutil.ReadAll(res.Body)
	jsonString := string(j)

	// initialize the result map which will hold only one entry for each account regardless of how many of the manager's
	// organization are permissioned to it
	accountMap := make(map[string]Account)

	// for each user's account collection
	result := gjson.Get(jsonString, "items.#.userAccountCollection.items.#.accountObject.items")
	result.ForEach(func(key, value gjson.Result) bool {
		// result JSON has extra array wrappers (for some reason) so we strip them so we can path it
		accountCollection := value.String()
		if len(accountCollection) > 2 {
			accountCollection = accountCollection[1 : len(accountCollection)-1]
		}

		// iterate over each account
		gjson.ForEachLine(accountCollection, func(line gjson.Result) bool {
			// result JSON has extra array wrappers (for some reason) so we strip them so we can path it
			account := line.String()
			if len(account) > 2 {
				account = account[1 : len(account)-1]
			}

			// If the account has not yet been added to the result map that add it, otherwise continue
			accountID := gjson.Get(account, "id").String()
			_, accountExists := accountMap[accountID]
			if !accountExists && len(accountID) > 0 {
				accountMap[accountID] = Account{
					AccountID:        accountID,
					AccountName:      LineOfBusinessMapping[gjson.Get(account, "accountLOB").String()],
					LOB:              gjson.Get(account, "accountName").String(),
					SolutionEngineer: gjson.Get(account, "createdBy").String(),
					NumOpportunities: gjson.Get(account, "opportunityCollection.count").String()}
			}

			return true // keep iterating
		})

		return true // keep iterating
	})

	// return map data as JSON
	accountArray := []Account{}
	for _, value := range accountMap {
		accountArray = append(accountArray, value)
	}
	accountJSON, err := json.Marshal(accountArray)
	if err != nil {
		return "", err
	}
	return string(accountJSON), nil
}

//
//  Load a global hashmap LOB key to description (e.g. enterprise, mid-market, etc)
//
func loadLinesOfBusiness() error {
	client := &http.Client{}

	// build query
	queryString := "?fields=id,lookupDescription&q=lookupType='LOB'&onlyData=true"
	req, _ := http.NewRequest("GET", GlobalConfig.ECALBaseURL+"Lookup"+queryString, nil)
	req.SetBasicAuth(GlobalConfig.VBCSUsername, GlobalConfig.VBCSPassword)

	// get data from VBCS Lookup service
	res, err := client.Do(req)
	if err != nil || res == nil || res.StatusCode != 200 {
		fmt.Println(outputHTTPError("Get LOB Lookup values", err, res))
		return err
	}
	defer res.Body.Close()
	json, _ := ioutil.ReadAll(res.Body)
	jsonString := string(json)

	// iterate over each item and add to the lookup map
	result := gjson.Get(jsonString, "items")
	for _, item := range result.Array() {
		id := gjson.Get(item.String(), "id").String()
		desc := gjson.Get(item.String(), "lookupDescription").String()
		LineOfBusinessMapping[id] = desc
	}

	return nil
}

//
// Wraps handler function with a basic auth check
//
type handler func(w http.ResponseWriter, r *http.Request)

func basicAuth(pass handler) handler {

	return func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()

		if username != GlobalConfig.ServiceUsername || password != GlobalConfig.ServicePassword {
			http.Error(w, "Authorization failed", http.StatusUnauthorized)
			return
		}

		pass(w, r)
	}
}

//
//  Read the config.json file and parse configuration data into a struct.  On error, panic here.
//
func loadConfig(filename string) Config {
	var config = Config{}
	file, err := os.Open(filename)
	if err != nil {
		panic(err.Error())
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		panic(err.Error())
	}

	return config
}

//
// Generic error formatting message for HTTP operations
//
func outputHTTPError(message string, err error, res *http.Response) string {
	if err != nil {
		return fmt.Sprintf("ERROR: %s: %s", message, err.Error())
	} else if res == nil {
		return fmt.Sprintf("ERROR: %s: %s", message, "HTTP Response is nil")
	} else {
		json, _ := ioutil.ReadAll(res.Body)
		return fmt.Sprintf("ERROR: %s: %s: detail ->%s", message, res.Status, string(json))
	}
}

//
// Helper function to print response body as a string
//
func printBody(res *http.Response) {
	bodyBytes, _ := ioutil.ReadAll(res.Body)
	fmt.Println(string(bodyBytes))
}
