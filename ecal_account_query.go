//  ECAL Account Query
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//
// HTTP handler for the getECALAccountQueryHandler functionality
//
func getECALAccountQueryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	instanceEnv := query.Get("instanceEnvironment")
	userEmail := query.Get("userEmail")
	isAdminString := query.Get("isAdmin")

	// convert isAdminString to a bool
	isAdmin := false
	if strings.ToLower(isAdminString) == "true" || strings.ToLower(isAdminString) == "yes" {
		isAdmin = true
	}

	// call the helper which does the data mashing
	result, err := getECALAccountQuery(instanceEnv, userEmail, isAdmin)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error in input parameters or processing; please contact your service administrator")
		fmt.Printf("***ERROR: %s\n", string(err.Error()))
		return
	}

	// format the result as json
	json := fmt.Sprintf("{\"items\": [%s]}", result)

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
}

//
// Returns data to power the ECAL application.  Specifically returns a list of accounts that should be presented to the user of the app.
// The instanceEnvironment identifier (sts-dev-preview, sts-prod-live, etc) is required to key the name of the ATP schema to query
// The userEmail parameter is either a manager or end-user email
// If the isAdmin paramter is set to true then all data will be returned
//
func getECALAccountQuery(instanceEnv string, userEmail string, isAdmin bool) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALAccountQuery: instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin))
		return "", errors.New(thisError)
	}

	// set the core query
	var template = `
	SELECT DISTINCT(a.id) as AccountID, 
		l.lookupdescription AS LOB, 
		a.accountname as AccountName, 
		a.createdby AS SolutionEngineer,
		(SELECT count(*) FROM %SCHEMA%.Opportunity o WHERE o.account = a.id) AS NumOpportunities
	FROM %SCHEMA%.User1 u 
	INNER JOIN %SCHEMA%.UserAccount ua ON ua.user1 = u.id
	INNER JOIN %SCHEMA%.Account a ON a.id = ua.account
	INNER JOIN %SCHEMA%.Lookup l ON l.id = a.accountlob AND l.lookuptype = 'LOB'	
	`
	// if the user is not an admin (regular user or manager) then append the hierarchical query suffix
	if isAdmin == false {
		template += `
		WHERE u.useremail = :1 OR u.manager in 
		(
		SELECT useremail 
		FROM VB_VB_B2ODNHURXIX.User1 u 
		INNER JOIN VB_VB_B2ODNHURXIX.RoleType r 
		ON u.rolename = r.id WHERE r.rolename = 'Manager' 
		START WITH useremail = :1 
		CONNECT BY PRIOR useremail = manager
		)
		`
	}

	// append the order by
	template += "ORDER BY AccountName ASC"

	// replace the %SCHEMA% template with the correct schema name
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])

	// run the query
	var rows *sql.Rows
	var err error
	if isAdmin {
		rows, err = DBPool.Query(query)
	} else {
		rows, err = DBPool.Query(query, userEmail)
	}
	if err != nil {
		thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALAccountQuery: Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var accountID, LOB, accountName, solutionEngineer, numOpportunities string

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&accountID, &LOB, &accountName, &solutionEngineer, &numOpportunities)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALAccountQuery: Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), err.Error())
			return "", errors.New(thisError)
		}
		result += fmt.Sprintf("{\"AccountID\": %s, \"LOB\": \"%s\", \"AccountName\": \"%s\", \"SolutionEngineer\": \"%s\", \"NumOpportunities\": %s},",
			accountID, LOB, accountName, solutionEngineer, numOpportunities)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	fmt.Printf("[%s] [%s] [%s] [%s] getECALAccountQuery: results=%d\n", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), count)
	return result, nil
}
