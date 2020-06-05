//  Main Server
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

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

	_ "github.com/godror/godror"
	"github.com/hashicorp/vault/api"
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
	IdentityMgrLeads          string
	InstanceEnvironments      string
	SchemaNames               string
	ECALOpportunitySyncTarget string
	ECALManagerHierarchyQuery string
	STSManagerHierarchyQuery  string
}

// GlobalConfig is a global holder for configuration information
var GlobalConfig Config

// DBPool is the database connection pool
var DBPool *sql.DB

// SchemaMap maps the instance-environment key (e.g. dev-stage, prod-live, etc) to the ATP schema name
var SchemaMap map[string]string

// IdentityMgrLeads contains the top level managers who should be included in the list of employees loaded into the platform
var IdentityMgrLeads []string

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
	println("Routing opportunity data to: " + GlobalConfig.ECALOpportunitySyncTarget)

	// initialize database connection pool
	DBPool, err = sql.Open("godror", GlobalConfig.DBConnectString)
	if err != nil {
		println(err)
		return
	}
	defer DBPool.Close()

	// register function listeners
	println("Registering REST handlers")
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/getManagerQuery", basicAuth(getManagerQueryHandler))
	http.HandleFunc("/getSTSManagerDashboardSummary", basicAuth(getSTSManagerDashboardSummaryHandler))
	http.HandleFunc("/getECALAccountQuery", basicAuth(getECALAccountQueryHandler))
	http.HandleFunc("/getECALOpportunityQuery", basicAuth(getECALOpportunityQueryHandler))
	http.HandleFunc("/getIdentities", basicAuth(getIdentitiesQueryHandler))
	http.HandleFunc("/postIdentities", basicAuth(postIdentitiesQueryHandler))
	http.HandleFunc("/postReferenceData", basicAuth(postReferenceDataHandler))

	// emit endpoint/database information
	dbuser := strings.SplitAfter(GlobalConfig.DBConnectString, "/")
	sid := strings.SplitAfter(GlobalConfig.DBConnectString, "@")
	fmt.Printf("Connecting to ATP Connect String: %s*******@%s\n", dbuser[0], sid[1])

	// start HTTP listener
	println("Starting HTTP Listener on port " + GlobalConfig.ServiceListenPort + "...\n")
	http.ListenAndServe(":"+GlobalConfig.ServiceListenPort, nil)
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

	// Convert identity manager leads into an array of strings
	IdentityMgrLeads = strings.Split(config.IdentityMgrLeads, ",")

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
