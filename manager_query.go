//  Health Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
		fmt.Fprintf(w, "Error in input parameters or processing; please contact your service administrator")
		fmt.Printf("***ERROR: %s\n", string(err.Error()))
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
		thisError := fmt.Sprintf("[%s] [%s] [%s] instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv, managerEmail)
		return "", errors.New(thisError)
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
		return "", errors.New(thisError)
	}
	defer rows.Close()

	var userEmail, queryString string

	// step through each row returned and add to the query filter using the correct format
	for rows.Next() {
		err := rows.Scan(&userEmail)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, err.Error())
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
