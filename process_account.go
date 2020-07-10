//  ProcessAccount
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper
//
// refer to https://golang.org/src/database/sql/sql_test.go for golang SQL samples

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// AccountLookup Represents an account returned from the corporate feed
type AccountLookup struct {
	CimID             string `json:"cim_id"`
	CimParentID       string `json:"cim_id_parent"`
	CimIDReg          string `json:"cim_id_reg"`
	AccountName       string `json:"account_name"`
	BusinessSegment   string `json:"bus_segment_str"`
	EndUserRegistryID string `json:"end_user_registry_id"`
	GlobalRegistryID  string `json:"end_user_orcl_glb_ult_reg_id"`
	RegistryIDList    string `json:"end_user_registry_id_str"`
	NacSeTeam         string `json:"nac_SE_Team"`
	NatSeTeam         string `json:"nat_SE_Team"`
}

//constants
const paygo = "PAYGO"

//
// Process accounts from JSON file to LookupAccount table
//
func processAccount(filename string) {

	// determine appropriate instance-environment based on the value of the config.json setting
	schema := SchemaMap[GlobalConfig.ECALOpportunitySyncTarget]
	if len(schema) < 1 {
		fmt.Printf("[%s] processAccount: Schema for [%s] not valid\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)
		return
	}

	// open file for reading
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("[%s] processAccount: Error opening file [%s]: %s\n",
			time.Now().Format(time.RFC3339), filename, err.Error())
		return
	}
	defer file.Close()

	// seek 10 bytes (chars) to advance past {"items":
	_, err = file.Seek(10, io.SeekStart)
	if err != nil {
		fmt.Printf("[%s] processAccount: Error advancing file stream to position 10: %s\n",
			time.Now().Format(time.RFC3339), err.Error())
		return
	}

	// decode full account list from response
	decoder := json.NewDecoder(file)
	fmt.Printf("[%s] [%s] processAccount: START Processing accounts\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)

	// start a DB transaction
	tx, err := DBPool.Begin()
	defer tx.Rollback()
	if err != nil {
		fmt.Printf("[%s] [%s] processAccount: Error creating DB transaction: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// delete all data from LookupAccount table
	_, err = tx.Exec("DELETE FROM " + schema + ".LookupAccount")
	if err != nil {
		fmt.Printf("[%s] [%s] processAccount: Unable to delete from LookupAccount: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// prepare insert statement
	insertStmt, err := tx.Prepare(
		"INSERT INTO " + schema + ".LookupAccount" +
			"(id, creationdate, lastupdatedate, createdby, lastupdatedby, abcschangenumber, " +
			"CimId, CimParentId, AccountName, BusinessSegment, EndUserRegistryId, GlobalRegistryId, " +
			"RegistryIdList, NacSeTeam, NatSeTeam, CimIDReg " +
			"VALUES(:1, SYSDATE, SYSDATE, 'cto_bizlogic_helper', 'cto_bizlogic_helper', null, " +
			":2, :3, :4, :5, :6, :7, :8, :9, :10, :11)")
	defer insertStmt.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processAccount: Unable to prepare statement for insert: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// consume the opening array brace
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("[%s] [%s] processAccount: Error decoding opening array token: %s\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// iterate each account
	counter := 1
	loaded := 0
	for decoder.More() {
		// decode next record
		var account AccountLookup
		err := decoder.Decode(&account)
		if err != nil {
			fmt.Printf("[%s] [%s] processAccount: Error decoding account %d: %s\n",
				time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter, err.Error())
			return
		}

		// perform any data adjustments necessary
		account.NacSeTeam = tokenizeSeList(account.NacSeTeam)
		account.NatSeTeam = tokenizeSeList(account.NatSeTeam)
		account.BusinessSegment = collapseBusinessSegment(account.BusinessSegment)

		// add account to LookupAccount staging table
		if account.BusinessSegment != paygo {
			_, err = insertStmt.Exec(counter, account.CimID, account.CimParentID, account.AccountName, account.BusinessSegment,
				account.EndUserRegistryID, account.GlobalRegistryID, account.RegistryIDList, account.NacSeTeam, account.NatSeTeam,
				account.CimIDReg)
			loaded++
		}
		if err != nil {
			fmt.Printf("[%s] [%s] processAccount: Unable to insert account %s into LookupAccount: %s\n", time.Now().Format(time.RFC3339),
				GlobalConfig.ECALOpportunitySyncTarget, account.AccountName, err.Error())
			return
		}

		counter++
	}

	// consume the closing array brace
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("[%s] [%s] processIdentity: Error decoding closing array token: %s\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// complete the transaction
	err = tx.Commit()
	if err != nil {
		fmt.Printf("[%s] [%s] processAccount: Error committing transaction: %s\n", time.Now().Format(time.RFC3339),
			GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	fmt.Printf("[%s] [%s] processAccount: DONE Processing %d accounts and loaded %d\n",
		time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter, loaded)
}

//
// Business segments from the internal system come with multiple segments per account as such:
// 	NATD ISV:NATD Public Sector
// 	NAC HQ:NAC SMB Cloud:NATD Public Sector
// 	NAC Midmarket Cloud:NATD Public Sector:NATD ULA License
// We will collapse these for our purposes into Key Account, Enterprise, Mid-Market, etc
// in that descending order of priority.  This means that there will be only a single business segment
// recorded per account.  If nothing matches our pick list then return an empty string.
// A special case is accounts marked NAC HQ.  These are paygos and we will ignore them in loading the DB
//
func collapseBusinessSegment(businessSegment string) string {
	if businessSegment == "NAC HQ" {
		return paygo
	}
	if strings.Contains(businessSegment, "Key Accounts") {
		return "Key Account"
	}
	if strings.Contains(businessSegment, "Enterprise") {
		return "Enterprise"
	}
	if strings.Contains(businessSegment, "Midmarket") {
		return "Mid-Market"
	}
	if strings.Contains(businessSegment, "SMB") {
		return "SMB"
	}
	if strings.Contains(businessSegment, "ISV") {
		return "ISV"
	}
	if strings.Contains(businessSegment, "Public Sector") {
		return "Public Sector"
	}
	return ""

}

//
// takes string in form of name1@email.com - ECA, name2@email.com - Hub SE, name3@email.com - CSM
// and returns a comma separated list of email addresses with spaces trimmed for easier processing by the front end
//
func tokenizeSeList(resourceList string) string {
	// handle 'null' token or empty data
	cleanList := ""
	if len(resourceList) < 1 || resourceList == "null" {
		return cleanList
	}

	// iterate through list
	for _, person := range strings.Split(resourceList, ",") {
		mySplit := strings.Split(person, "-")
		if len(mySplit) == 2 {
			cleanList = cleanList + strings.TrimSpace(mySplit[0]) + "-" + strings.TrimSpace(mySplit[1]) + ","
		}
	}

	// remove final comma if it exists
	if len(cleanList) > 1 {
		cleanList = cleanList[0 : len(cleanList)-1]
	}
	return cleanList
}
