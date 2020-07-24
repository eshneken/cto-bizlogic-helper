//  Main Server
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"strings"

	_ "github.com/godror/godror"
	"github.com/oracle/oci-go-sdk/common"
	"github.com/oracle/oci-go-sdk/common/auth"
	"github.com/oracle/oci-go-sdk/secrets"
)

// Config holds all config data loaded from local config.json file
type Config struct {
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

	// check to see if we should skip config decoding w/ OCI Secrets Service by looking for the --novault flag
	// use this for local testing where unencrypted config files are used
	skipVault := false
	if len(os.Args) > 1 {
		if os.Args[1] == "--novault" {
			skipVault = true
			println("Running in LOCAL mode with NO OCI Secrets Service integration.")
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
	http.HandleFunc("/getECALArtifactQuery", basicAuth(getECALArtifactQueryHandler))
	http.HandleFunc("/getECALDataQuery", basicAuth(getECALDataQueryHandler))
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
//  Read the config.json file and parse configuration data into a struct. Communicate with the OCI Secrets Service
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

	// connect to the OCI Secrets Service
	var provider common.ConfigurationProvider
	provider, err = auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		provider = common.DefaultConfigProvider()
	}

	client, err := secrets.NewSecretsClientWithConfigurationProvider(provider)
	if err != nil {
		panic("connecting to OCI Secrets Service: " + err.Error())
	}

	// step through all the struct values and scan for [vault] prefix
	// which indicates that the value needs to be retrieved from the OCI Secret Service
	// format is [vault]FieldName:OCID
	v := reflect.ValueOf(config)
	values := make([]interface{}, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		values[i] = v.Field(i).Interface()
		if strings.HasPrefix(values[i].(string), "[vault]") {
			keySlice := strings.Split(strings.TrimPrefix(values[i].(string), "[vault]"), ":")
			fieldName := keySlice[0]
			vaultKey := keySlice[1]
			vaultValue := getSecretValue(client, vaultKey)
			reflect.ValueOf(&config).Elem().FieldByName(fieldName).SetString(vaultValue)
		}
	}

	return config
}

//
// Returns a secret value from the OCI Secret Service based on a secret OCID
//
func getSecretValue(client secrets.SecretsClient, secretOCID string) string {
	request := secrets.GetSecretBundleRequest{SecretId: &secretOCID}
	response, err := client.GetSecretBundle(context.Background(), request)
	if err != nil {
		panic("reading value for key [" + secretOCID + "]: " + err.Error())
	}

	encodedResponse := fmt.Sprintf("%s", response.SecretBundleContent)
	encodedResponse = strings.TrimRight(strings.TrimLeft(encodedResponse, "{ Content="), " }")
	decodedByteArray, err := base64.StdEncoding.DecodeString(encodedResponse)
	if err != nil {
		panic("decoding value for key [" + secretOCID + "]: " + err.Error())
	}

	return string(decodedByteArray)
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
