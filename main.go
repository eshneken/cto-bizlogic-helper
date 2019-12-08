//	CTO ECAL Business Logic Helpers
//	Ed Shnekendorf, 2019, https://github.com/eshneken/cto-ecal-bizlogic

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	_ "gopkg.in/goracle.v2"
)

// Config holds all config data loaded from local config.json file
type Config struct {
	ServiceListenPort     string
	ServiceUsername       string
	ServicePassword       string
	DBConnectString       string
	ManagerHierarchyQuery string
	InstanceEnvironments  string
	SchemaNames           string
	VBCSUsername          string
	VBCSPassword          string
	ECALBaseURL           string
	IdentityFilename      string
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

// DBPool is the database connection pool
var DBPool *sql.DB

// LineOfBusinessMapping maps LOB types to descriptions
var LineOfBusinessMapping map[string]string

// SchemaMap maps the instance-environment key (e.g. dev-stage, prod-live, etc) to the ATP schema name
var SchemaMap map[string]string

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

	// load schema mappings
	println("Loading schema mappings")
	SchemaMap = make(map[string]string)
	err = loadSchemaMap()
	if err != nil {
		println(err)
		return
	}

	// initialize database connection pool
	DBPool, err = sql.Open("goracle", GlobalConfig.DBConnectString)
	if err != nil {
		println(err)
		return
	}
	defer DBPool.Close()

	// register function listeners
	println("Registering REST handlers")
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/getAccounts", basicAuth(getAccountHandler))
	http.HandleFunc("/getManagerQuery", basicAuth(getManagerQueryHandler))
	http.HandleFunc("/getIdentities", basicAuth(getIdentitiesQueryHandler))
	http.HandleFunc("/postIdentities", basicAuth(postIdentitiesQueryHandler))

	// emit endpoint/database information
	println("Connecting to VBCS Endpoint: " + GlobalConfig.ECALBaseURL)
	dbuser := strings.SplitAfter(GlobalConfig.DBConnectString, "/")
	sid := strings.SplitAfter(GlobalConfig.DBConnectString, "@")
	fmt.Printf("Connecting to ATP Connect String: %s*******@%s\n", dbuser[0], sid[1])

	// start HTTP listener
	println("Starting HTTP Listener on port " + GlobalConfig.ServiceListenPort + "...\n")
	http.ListenAndServe(":"+GlobalConfig.ServiceListenPort, nil)
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
// HTTP handler that writes the contents of the identities file to the output
//
func postIdentitiesQueryHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Println(outputHTTPError("postIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
		return
	}
	// write identities to filesystem
	err = ioutil.WriteFile(GlobalConfig.IdentityFilename, body, 0700)
	if err != nil {
		fmt.Println(outputHTTPError("postIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
	}
}

//
// HTTP handler that writes the contents of the identities file to the output
//
func getIdentitiesQueryHandler(w http.ResponseWriter, r *http.Request) {
	// open identities JSON file from filesystem
	data, err := ioutil.ReadFile(GlobalConfig.IdentityFilename)
	if err != nil {
		fmt.Println(outputHTTPError("getIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
		return
	}

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(data))
}

//
// HTTP handler for the getManagerQuery functionality
//
func getManagerQueryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	managerEmail := query.Get("managerEmail")
	instanceEnv := query.Get("instanceEnvironment")

	// call the helper which does the data mashing
	result, err := getManagerQuery(managerEmail, instanceEnv)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, string(err.Error()))
		return
	}

	// format the result as json
	json := fmt.Sprintf("{\"query\":\"%s\"}", result)

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
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
		return
	}

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
}

//
// Returns a VBCS query string that lists all managers within a given manager's hierarchy.  Given a manager's email address
// which is provided as a query parameter (managerEmail) return all other managers below this manager in the reporting structure
// in the form of "manager = '".  In addition to the manager email, the instanceEnvironment identifier (dev-preview, prod-live, etc)
// is required to key the name of the ATP schema to query
//
func getManagerQuery(managerEmail string, instanceEnv string) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		return "", errors.New("instanceEnvironment query parameter is invalid")
	}
	query := strings.ReplaceAll(GlobalConfig.ManagerHierarchyQuery, "%SCHEMA%", SchemaMap[instanceEnv])

	// run the query
	rows, err := DBPool.Query(query, managerEmail)
	if err != nil {
		thisError := fmt.Sprintf("[%s] [%s] [%s] Error running query: %s", time.Now(), instanceEnv, managerEmail, err.Error())
		fmt.Println(thisError)
		return "", errors.New(thisError)
	}
	defer rows.Close()

	var userEmail, queryString string

	// step through each row returned and add to the query filter using the correct format
	for rows.Next() {
		err := rows.Scan(&userEmail)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] Error scanning row: %s", time.Now(), instanceEnv, managerEmail, err.Error())
			fmt.Println(thisError)
			return "", errors.New(thisError)
		}
		queryString += fmt.Sprintf("manager = '%s' or ", userEmail)
	}

	// string the trailing 'or' field if it exists
	queryString = strings.TrimSuffix(queryString, "or ")

	// if we didn't get any results, just use the email address that was passed in for the query
	// this shouldn't happen but if it does this will fail gracefully
	if len(queryString) < 1 {
		queryString = fmt.Sprintf("manager = '%s'", managerEmail)
	}

	fmt.Printf("[%s] [%s] [%s] Query: %s\n", time.Now(), instanceEnv, managerEmail, queryString)
	return queryString, nil
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
	queryString := "?fields=id,userEmail;userAccountCollection.accountObject:id,accountName,accountLOB,createdBy;userAccountCollection.accountObject.opportunityCollection:id,opportunityID&limit=9000&onlyData=true"
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
					LOB:              LineOfBusinessMapping[gjson.Get(account, "accountLOB").String()],
					AccountName:      gjson.Get(account, "accountName").String(),
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

	// sort the array by account name
	sort.Slice(accountArray[:], func(i, j int) bool {
		return accountArray[i].AccountName < accountArray[j].AccountName
	})

	// return the JSON representation
	accountJSON, err := json.Marshal(accountArray)
	if err != nil {
		return "", err
	}
	return string(accountJSON), nil
}

//
//  Load a global hashmap to map instance-env to ATP schema name
//
func loadSchemaMap() error {
	instanceEnvKeys := strings.Split(GlobalConfig.InstanceEnvironments, ",")
	schemaNames := strings.Split(GlobalConfig.SchemaNames, ",")
	if len(instanceEnvKeys) != len(schemaNames) {
		return errors.New("InstanceEnvironments count doesn't match SchemaNames count.  Check config.json")
	}

	// create a hashmap for easier runtime lookup
	for i, item := range instanceEnvKeys {
		SchemaMap[item] = schemaNames[i]
		println("\t" + item + " -> " + SchemaMap[item])
	}

	return nil
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
