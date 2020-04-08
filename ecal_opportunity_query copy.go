//  ECAL Opportunity Query
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
func getECALOpportunityQueryHandler(w http.ResponseWriter, r *http.Request) {
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
	result, err := getECALOpportunityQuery(instanceEnv, userEmail, isAdmin)
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
func getECALOpportunityQuery(instanceEnv string, userEmail string, isAdmin bool) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALOpportunityQuery: instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin))
		return "", errors.New(thisError)
	}

	// set the core query
	var template = `
		SELECT  DISTINCT(o.id) AS ID,
			a.id AS AccountID, 
			a.accountname AS AccountName, 
			o.opportunityid AS OpportunityID,
			o.summary AS Summary,
			NVL(o.projectedARR, 0) AS ARR,
			NVL(o.ecalPercentComplete, 0) AS ECALPercent,
			NVL(stg.stage, 'None') AS LatestECALStage,
			TO_CHAR(o.lastupdatedate, 'MM/DD/YYYY') AS LastActivity,
			NVL(o.pocRequired, 0) AS POC,
			NVL(l.lookupdescription, 'None') AS POCStatus,
			NVL(o.commercialBlockers, 0) AS CommercialBlockers,
			NVL(o.technicalBlockers, 0) AS TechnicalBlockers
		FROM %SCHEMA%.User1 u 
		INNER JOIN %SCHEMA%.UserAccount ua ON ua.user1 = u.id
		INNER JOIN %SCHEMA%.Account a ON a.id = ua.account
		INNER JOIN %SCHEMA%.Opportunity o ON o.account = a.id
		LEFT OUTER JOIN %SCHEMA%.ECALStage stg ON stg.id = o.lateststagedone
		LEFT OUTER JOIN %SCHEMA%.Lookup l ON l.id = o.pocstatus AND l.lookuptype = 'POC_STATUS'	
	`
	// if the user is not an admin (regular user or manager) then append the hierarchical query suffix
	if isAdmin == false {
		template += `
		WHERE u.useremail = :1 OR u.manager in 
		(
		SELECT useremail 
		FROM %SCHEMA%.User1 u 
		INNER JOIN %SCHEMA%.RoleType r 
		ON u.rolename = r.id WHERE r.rolename = 'Manager' 
		START WITH useremail = :1 
		CONNECT BY PRIOR useremail = manager
		)
		`
	}

	// append the order by
	template += "ORDER BY AccountName ASC, OpportunityID ASC"

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
		thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALOpportunityQuery: Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var id, accountID, accountName, opportunityID, summary, arr, ecalPercent, latestECALStage, lastActivity, pocStatus string
	var commercialBlockers, technicalBlockers, poc int

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&id, &accountID, &accountName, &opportunityID, &summary, &arr, &ecalPercent, &latestECALStage, &lastActivity, &poc, &pocStatus, &commercialBlockers, &technicalBlockers)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] [%s] getECALOpportunityQuery: Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), err.Error())
			return "", errors.New(thisError)
		}

		// calculate booleans
		blockers := false
		if commercialBlockers == 1 || technicalBlockers == 1 {
			blockers = true
		}
		pocBool := false
		if poc == 1 {
			pocBool = true
		}

		result += fmt.Sprintf("{\"ID\": %s, \"AccountID\": %s, \"AccountName\": \"%s\", \"OpportunityID\": \"%s\", \"Summary\": \"%s\", \"ARR\": %s, \"ECALPercent\": %s, \"LatestECALStage\": \"%s\", \"LastActivity\": \"%s\", \"POC\": %t, \"POCStatus\": \"%s\", \"Blockers\": %t},",
			id, accountID, accountName, opportunityID, summary, arr, ecalPercent, latestECALStage, lastActivity, pocBool, pocStatus, blockers)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	fmt.Printf("[%s] [%s] [%s] [%s] getECALOpportunityQuery: results=%d\n", time.Now().Format(time.RFC3339), instanceEnv, userEmail, strconv.FormatBool(isAdmin), count)
	return result, nil
}
