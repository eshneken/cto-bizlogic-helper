//  ProcessOpportunity
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
	"strconv"
	"strings"
	"time"
)

// OpportunityLookup represents an individual returned from the custom Aria export service
type OpportunityLookup struct {
	OppID                 string `json:"opportunity_id"`
	OppName               string `json:"opportunity_name"`
	OppOwner              string `json:"opportunity_owner"`
	TerritoryOwner        string `json:"territory_owner"`
	OppStatus             string `json:"opportunity_status"`
	CloseDate             string `json:"close_date"`
	CustomerName          string `json:"customer_name"`
	WinProbability        string `json:"opp_probability"`
	IntegrationID         string `json:"opty_int_id"`
	RegistryID            string `json:"registry_id"`
	CimID                 string `json:"cim_id"`
	OpportunityValue      string `json:"oppty_amount_k"`
	ForecastTypeGroup     string `json:"forecast_type_group"`
	RevenueLineID         string `json:"revenue_line_id"`
	RevenueType           string `json:"revenue_type"`
	RevenueTypeGroup      string `json:"revenue_type_group"`
	RevenueLineStatus     string `json:"revenue_line_status"`
	RevenueSalesStage     string `json:"rev_sales_stage"`
	RevenuePipelineK      string `json:"rev_pipeline_k"`
	RevenueTCVK           string `json:"rev_tcv_k"`
	RevenueProbability    string `json:"rev_probability"`
	ProductClass          string `json:"product_class"`
	ProductPillar         string `json:"product_pillar"`
	ProductLine           string `json:"product_line"`
	ProductGroup          string `json:"product_group"`
	ProductName           string `json:"product_name"`
	ProductDescription    string `json:"product_description"`
	WorkloadAmount        string `json:"opp_total_workload_k"`
	ConsumptionStartDate  string `json:"consumption_start_date"`
	ConsumptionRampMonths string `json:"cons_ramp_months"`
	L2TerritoryName       string `json:"level_2_territory_name"`
	L3TerritoryName       string `json:"level_3_territory_name"`
	L2TerritoryEmail      string `json:"level_2_territory_owner_email"`
	L3TerritoryEmail      string `json:"level_3_territory_owner_email"`
}

//
// Process opportunities from JSON file to LookupOpportunity table
//
func processOpportunity(filename string) {

	// determine appropriate instance-environment based on the value of the config.json setting
	schema := SchemaMap[GlobalConfig.ECALOpportunitySyncTarget]
	if len(schema) < 1 {
		fmt.Printf("[%s] processOpportunity: Schema for [%s] not valid\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)
		return
	}

	// open file for reading
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("[%s] processOpportunity: Error opening file [%s]: %s\n",
			time.Now().Format(time.RFC3339), filename, err.Error())
		return
	}
	defer file.Close()

	// seek 10 bytes (chars) to advance past {"items":
	_, err = file.Seek(10, io.SeekStart)
	if err != nil {
		fmt.Printf("[%s] processOpportunity: Error advancing file stream to position 10: %s\n",
			time.Now().Format(time.RFC3339), err.Error())
		return
	}

	// decode full opportunity list from response
	decoder := json.NewDecoder(file)
	fmt.Printf("[%s] [%s] processOpportunity: START Processing opportunities\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)

	// start a DB transaction
	tx, err := DBPool.Begin()
	defer tx.Rollback()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Error creating DB transaction: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// delete all data from LookupOpportunity table
	_, err = tx.Exec("DELETE FROM " + schema + ".LookupOpportunity")
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to delete from LookupOpportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// prepare insert statement
	queryString := "INSERT INTO " + schema + ".LookupOpportunity" +
		"(id, creationdate, lastupdatedate, createdby, lastupdatedby, abcschangenumber, " +
		"opportunityid, summary, salesrep, projectedarr, anticipatedclosedate, winprobability, " +
		"projectedtcv, integrationid, registryid, cimid, opportunitystatus, customername, territoryowner," +
		"opportunityvalue, forecasttypegroup, revenuelineid, revenuetype, revenuetypegroup, revenuelinestatus, revenuesalesstage," +
		"revenuepipelinek, revenuetcvk, revenueprobability, productclass, productpillar, productline, productgroup," +
		"productname, productdescription, workloadamount, consumptionstartdate, consumptionrampmonths, l2territoryname, l3territoryname, l2territoryemail, l3territoryemail" +
		") VALUES ( " +
		":1, SYSDATE, SYSDATE, 'cto_bizlogic_helper', 'cto_bizlogic_helper', null, " +
		":2, :3, :4, :5, TO_DATE(:6, 'YYYY-MM-DD'), :7, " +
		":8, :9, :10, :11, :12, :13, :14, " +
		":15, :16, :17, :18, :19, :20, :21, " +
		":22, :23, :24, :25, :26, :27, :28, " +
		":29, :30, :31, TO_DATE(:32, 'YYYY-MM-DD'), :33, :34, :35, :36, :37)"
	insertStmt, err := tx.Prepare(queryString)
	defer insertStmt.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to prepare statement for insert: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// prepare update statements for Opportunity & OpportunityWorkload
	updateStmt1, err := tx.Prepare(
		"UPDATE " + schema + ".Opportunity SET" +
			" summary = :1, salesRep = :2, projectedARR = :3, projectedTCV = :4, opportunityStatus = :5, anticipatedCloseDate = TO_DATE(:6, 'YYYY-MM-DD'), " +
			" winProbability = :7, lastUpdatedBy = 'cto_bizlogic_helper', lastUpdateDate = SYSDATE WHERE opportunityID = :8")
	defer updateStmt1.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to prepare statement for Opportunity update: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}
	updateStmt2, err := tx.Prepare(
		"UPDATE " + schema + ".OpportunityWorkload SET" +
			" WorkloadDescription = :1, ConsumptionStartDate = TO_DATE(:2, 'YYYY-MM-DD'), ConsumptionRampMonths = :3, WorkloadType = :4, lastUpdatedBy = 'cto_bizlogic_helper', lastUpdateDate = SYSDATE " +
			" WHERE id = (SELECT w.id FROM " + schema + ".OpportunityWorkload w INNER JOIN " + schema + ".Opportunity o ON o.id = w.opportunity " +
			" WHERE o.opportunityid = :5 and w.workloadidentifier = :6)")
	defer updateStmt2.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to prepare statement for OpportunityWorkload update: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// consume the opening array brace
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Error decoding opening array token: %s\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// iterate each opportunity
	insertedOpps := 0
	counter := 1
	for decoder.More() {
		// decode next record
		var opp OpportunityLookup
		err := decoder.Decode(&opp)
		if err != nil {
			fmt.Printf("[%s] [%s] processOpportunity: Error decoding person %d: %s\n",
				time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter, err.Error())
			return
		}

		// convert strings to numbers
		tcv := 0
		arr := 0
		opportunityValue, _ := strconv.ParseFloat(opp.OpportunityValue, 64)
		revenuePipelineK, _ := strconv.ParseFloat(opp.RevenuePipelineK, 64)
		revenueTCVK, _ := strconv.ParseFloat(opp.RevenueTCVK, 64)
		workloadAmount, _ := strconv.ParseFloat(opp.WorkloadAmount, 64)
		consumptionRampMonths, _ := strconv.ParseFloat(opp.ConsumptionRampMonths, 64)
		winProbability, _ := strconv.ParseInt(opp.WinProbability, 10, 64)
		workloadProbability, _ := strconv.ParseInt(opp.RevenueProbability, 10, 64)

		// truncate timestamps
		opp.CloseDate = strings.TrimSuffix(strings.Split(opp.CloseDate, "T")[0], "T")
		opp.ConsumptionStartDate = strings.TrimSuffix(strings.Split(opp.ConsumptionStartDate, "T")[0], "T")

		// other fixes for the evil that SI data brings
		if len(opp.OppOwner) < 1 {
			opp.OppOwner = opp.TerritoryOwner
		}
		if workloadProbability == 0 {
			workloadProbability = winProbability
		}

		// add opportunity to LookupOpportunity staging table
		// only put opportunities in 'Open' or 'Won' state into the lookup table
		if opp.OppStatus == "Open" || opp.OppStatus == "Won" {
			_, err = insertStmt.Exec(
				counter, opp.OppID, opp.OppName, opp.OppOwner, arr*1000, opp.CloseDate, winProbability,
				tcv*1000, opp.IntegrationID, opp.RegistryID, opp.CimID, opp.OppStatus, opp.CustomerName, opp.TerritoryOwner,
				opportunityValue*1000, opp.ForecastTypeGroup, opp.RevenueLineID, opp.RevenueType, opp.RevenueTypeGroup, opp.RevenueLineStatus, opp.RevenueProbability,
				revenuePipelineK*1000, revenueTCVK*1000, workloadProbability, opp.ProductClass, opp.ProductPillar, opp.ProductLine, opp.ProductGroup,
				opp.ProductName, opp.ProductDescription, workloadAmount*1000, opp.ConsumptionStartDate, consumptionRampMonths, opp.L2TerritoryName, opp.L3TerritoryName, opp.L2TerritoryEmail, opp.L3TerritoryEmail)
			if err != nil {
				fmt.Printf("[%s] [%s] processOpportunity: Unable to insert opportunity %s into LookupOpportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, opp.OppID, err.Error())
				return
			}
			insertedOpps++
		}

		// update existing Opportunity table with any updated data.  We do this regardless of opportunity status since
		// this will allow us to 'close' previously open opportunities
		_, err = updateStmt1.Exec(opp.OppName, opp.OppOwner, arr*1000, tcv*1000, opp.OppStatus, opp.CloseDate, winProbability, opp.OppID)
		if err != nil {
			fmt.Printf("[%s] [%s] processOpportunity: Unable to update opportunity %s in Opportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, opp.OppID, err.Error())
			return
		}
		// update existing OpportunityWorkload table with any updated data.  We do this regardless of opportunity status since
		// this will allow us to 'close' previously open opportunities
		_, err = updateStmt2.Exec(opp.ProductDescription, opp.ConsumptionStartDate, consumptionRampMonths, opp.ProductGroup, opp.OppID, opp.RevenueLineID)
		if err != nil {
			fmt.Printf("[%s] [%s] processOpportunity: Unable to update opportunity %s in OpportunityWorkload: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, opp.OppID, err.Error())
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
		fmt.Printf("[%s] [%s] processOpportunity: Error committing transaction: %s\n", time.Now().Format(time.RFC3339),
			GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	fmt.Printf("[%s] [%s] processOpportunity: DONE Processing %d opportunities with %d in Open/Won state\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter-1, insertedOpps)
}
