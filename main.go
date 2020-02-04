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
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
	_ "gopkg.in/goracle.v2"
)

// Config holds all config data loaded from local config.json file
type Config struct {
	VaultAddress              string
	VaultCli                  string
	VaultRole                 string
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
	println("CTO-Bizlogic-Helper says w00t!")

	// check to see if we should skip config decoding w/ HashiCorp Vault by looking for the --novault flag
	// use this for local testing where unencrypted config files are used
	skipVault := false
	if len(os.Args) > 1 {
		if os.Args[1] == "--novault" {
			skipVault = true
			println("Running in LOCAL mode with NO HashiCorp Vault integration.")
		}
	}

	// read system configuration from config file
	println("Reading & Decoding config.json")
	GlobalConfig = loadConfig("config.json", skipVault)

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
	healthy := true

	// make sure the database connection can be made
	rows, err := DBPool.Query("SELECT SYSDATE FROM DUAL")
	if err != nil {
		thisError := fmt.Sprintf("[%s] DB healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthy = false
	}
	defer rows.Close()

	// make sure identity filename exists and is readable
	_, err = ioutil.ReadFile(GlobalConfig.IdentityFilename)
	if err != nil {
		thisError := fmt.Sprintf("[%s] FILE healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthy = false
	}

	// write appropriate response code based on health condition
	if healthy {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		fmt.Fprintf(w, "HEALTH_OK")
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(500)
		fmt.Fprintf(w, "HEALTH_NOT_OK")
	}
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
		thisError := fmt.Sprintf("[%s] [%s] [%s] Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, err.Error())
		fmt.Println(thisError)
		return "", errors.New(thisError)
	}
	defer rows.Close()

	var userEmail, queryString string

	// step through each row returned and add to the query filter using the correct format
	for rows.Next() {
		err := rows.Scan(&userEmail)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, err.Error())
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

	fmt.Printf("[%s] [%s] [%s] Query: %s\n", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, queryString)
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
//  Read the config.json file and parse configuration data into a struct. Communicate with the HashiCorp Vault server
//  to retrieve the secret data.  If the environment is secure and vault is not needed, that section can be skipped by
//  passing skipVault=true.  On error, panic here.
//
func loadConfig(filename string, skipVault bool) Config {

	// open the config file
	var config = Config{}
	file, err := os.Open(filename)
	if err != nil {
		panic("reading config.json: " + err.Error())
	}
	defer file.Close()

	// decode config.json into struct
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		panic("marshalling to struct: " + err.Error())
	}

	// if vault integration is off, return the config struct as-is.  no need for further decoding.
	if skipVault == true {
		return config
	}

	// connect to HashiCorp Vault
	vaultConfig := &api.Config{Address: config.VaultAddress}
	hashiClient, err := api.NewClient(vaultConfig)
	if err != nil {
		panic("connecting to Vault[" + config.VaultAddress + "]: " + err.Error())
	}

	// get Vault token
	vaultToken, err := getVaultToken(config)
	if err != nil {
		panic("authenticating & getting token: " + err.Error())
	}
	hashiClient.SetToken(vaultToken)

	// step through all the struc values and scan for [vault] prefix
	// which indicates that the value needs to be retrieved from a HashiCorp
	// vault server
	v := reflect.ValueOf(config)
	values := make([]interface{}, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		values[i] = v.Field(i).Interface()
		if strings.HasPrefix(values[i].(string), "[vault]") {
			vaultKey := strings.TrimPrefix(values[i].(string), "[vault]")
			vaultValue, err := hashiClient.Logical().Read("cto/" + vaultKey)
			if vaultValue == nil || err != nil {
				panic("reading value for key [" + vaultKey + "]: " + err.Error())
			}
			reflect.ValueOf(&config).Elem().FieldByName(vaultKey).SetString(vaultValue.Data["value"].(string))
		}
	}

	return config
}

//
// getVaultToken authenticates against HashiCorp Vault using OCI instance principal credentials, scans the output
// and returns the session token to be used by the Hashi client.
//
func getVaultToken(config Config) (string, error) {
	// authenticate against Vault using instance principal credentials
	stdout, err := exec.Command(config.VaultCli, "login", "-address="+config.VaultAddress,
		"-method=oci", "auth_type=instance", "role="+config.VaultRole).Output()
	if err != nil {
		return "", err
	}

	// scan through the vault cli output and scan for the standalone token line
	// not that the HasPrefix method includes a space in the pattern after token
	// to get the correct value
	token := ""
	output := strings.Split(string(stdout), "\n")
	for _, line := range output {
		if strings.HasPrefix(line, "token ") {
			fields := strings.Fields(line)
			token = fields[1]
		}
	}

	return token, nil
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
