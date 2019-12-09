//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2019, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	_ "gopkg.in/goracle.v2"
)

// Config holds all config data loaded from local config.json file
type Config struct {
	ServiceListenPort         string
	ServiceUsername           string
	ServicePassword           string
	DBConnectString           string
	IdentityFilename          string
	InstanceEnvironments      string
	SchemaNames               string
	ECALManagerHierarchyQuery string
	STSManagerHierarchyQuery  string
}

// GlobalConfig is a global holder for configuration information
var GlobalConfig Config

// DBPool is the database connection pool
var DBPool *sql.DB

// SchemaMap maps the instance-environment key (e.g. dev-stage, prod-live, etc) to the ATP schema name
var SchemaMap map[string]string

func main() {
	// read system configuration from config file
	GlobalConfig = loadConfig("config.json")

	// load schema mappings
	println("Loading schema mappings")
	SchemaMap = make(map[string]string)
	err := loadSchemaMap()
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
	http.HandleFunc("/getManagerQuery", basicAuth(getManagerQueryHandler))
	http.HandleFunc("/getIdentities", basicAuth(getIdentitiesQueryHandler))
	http.HandleFunc("/postIdentities", basicAuth(postIdentitiesQueryHandler))

	// emit endpoint/database information
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

	// based on the instanceEnvironment key, choose the right schema and query type
	// depending on whether the caller is ECAL or STS and what environment we're in
	var template string
	if strings.HasPrefix(instanceEnv, "ecal-") {
		template = GlobalConfig.ECALManagerHierarchyQuery
	} else {
		template = GlobalConfig.STSManagerHierarchyQuery
	}
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])

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
