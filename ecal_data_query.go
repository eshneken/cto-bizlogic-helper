//  ECAL Data Query
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
// HTTP handler for the getECALDataQueryHandler functionality
//
func getECALDataQueryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	instanceEnv := query.Get("instanceEnvironment")

	// call the helper which does the data mashing
	result, err := getECALDataQuery(instanceEnv)
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
func getECALDataQuery(instanceEnv string) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("[%s] [%s] getECALDataQuery: instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv)
		return "", errors.New(thisError)
	}

	// set the core query
	var template = `with function calculateColor(csa number, cp number, cs number, fs number, sec number, tech number, poc number, ccInvolved number, ccSar number) return char is
    totalCount number := 6;
    score number := csa + cp + cs+ fs + sec + tech;
		begin
			if poc = 1 then 
				totalCount := totalCount + 1;
				score := score + poc;
			end if;
			if ccInvolved = 1 then
				totalCount := totalCount + 1;
				score := score + ccSar;
			end if;
			if score <=2 then
				return 'R';
			elsif score > 2 and score < totalCount then
				return 'Y';
			else
				return 'G';
			end if;
		end;
		select  
			distinct(o.id) as ecal_workload_id,
			a.id as ecal_account_id,
			o.opportunityid as opportunity_id,
			nvl(w.workloadtype, 'None') as workload_type,
			w.workloadidentifier as workload_identifier,
			a.accountname as account_name,
			a.cimid as cim_id,
			o.summary as workload_summary,    
			calculateColor(
				nvl(a.currentcsaexecuted, 0), 
				(select ora3.done FROM %SCHEMA%.opportunityrequiredarti ora3 INNER JOIN %SCHEMA%.requiredartifacts ra3 ON ora3.requiredartifact = ra3.id where o.id = ora3.opportunity and ra3.name = 'Consumption Plan'), 
				(select ora2.done FROM %SCHEMA%.opportunityrequiredarti ora2 INNER JOIN %SCHEMA%.requiredartifacts ra2 ON ora2.requiredartifact = ra2.id where o.id = ora2.opportunity and ra2.name = 'Architecture Diagram'), 
				(select ora1.done FROM %SCHEMA%.opportunityrequiredarti ora1 INNER JOIN %SCHEMA%.requiredartifacts ra1 ON ora1.requiredartifact = ra1.id where o.id = ora1.opportunity and ra1.name = 'Logical Architecture'), 
				nvl(th.securitysignoffdone, 0), 
				nvl(th.technicalsignoffdone, 0), 
				nvl(th.pocrequired, 0), 
				nvl(th.cloudatcustomerinvolved, 0), 
				nvl(th.cloudatcustomersardone, 0)) 
			as color,
			nvl((select stage FROM %SCHEMA%.EcalStage where id = o.lateststagedone), 'None') as latest_ecal_stage_done,
			nvl(a.currentcsaexecuted, 0) as csa_executed,
			o.technicallead as tech_lead,
			u.manager as tech_manager,
			nvl(th.pocrequired, 0) as poc_required,
			to_char(th.pocenddate, 'MM-DD-YYYY') as poc_enddate,
			nvl(th.pocstatus, 'Not Started') as poc_status,
			nvl(th.pocresolution, 'None') as poc_resolution,
			nvl(th.securitysignoffdone, 0) as security_signoff,
			nvl(th.technicalsignoffdone, 0) as technical_signoff,
			nvl(th.consumptionplansignoff, 0) as cons_plan_signoff,
			nvl(th.cloudatcustomerinvolved, 0) as cc_involved,
			nvl(th.cloudatcustomersardone,0) as cc_done,
			nvl(th.technicalblockers, 0) as tech_blockers,
			nvl(th.commercialblockers, 0) as commercial_blockers,
			nvl(th.coronavirusimpact, 0) as covid_impact,
			nvl(th.oracleconsultingengaged, 0) as ocs_engaged,
			nvl(th.expansion, 0) as expansion,
			th.technicaldecisionmakern as tech_decider,
			to_char(th.technicalsignoffdate, 'MM-DD-YYYY') as tech_signoff_date,
			th.migrationrunby as migration_by,
			th.tigerseemail as tiger_se_email,
			th.partnername as partner_name,
			th.workloadprogressionstage as workload_progression,
			th.adoptionowneremail as adopter_email,
			th.adoptionownernametitle as adopter_name,
			th.implementeremail as implementer_email,
			th.implementernametitle as implementer_name,
			(select ora1.done
			FROM %SCHEMA%.OpportunityRequiredArti ora1
			INNER JOIN %SCHEMA%.RequiredArtifacts ra1 ON ora1.requiredartifact = ra1.id
			where o.id = ora1.opportunity and ra1.name = 'Logical Architecture') as future_state_complete,
			nvl(((select ora2.done
			FROM %SCHEMA%.OpportunityRequiredArti ora2
			INNER JOIN %SCHEMA%.RequiredArtifacts ra2 ON ora2.requiredartifact = ra2.id
			where o.id = ora2.opportunity and ra2.name = 'Architecture Diagram') intersect (select ora21.done
			FROM %SCHEMA%.OpportunityRequiredArti ora21
			INNER JOIN %SCHEMA%.RequiredArtifacts ra21 ON ora21.requiredartifact = ra21.id
			where o.id = ora21.opportunity and ra21.name = 'Inventory Spreadsheet')), 0) as current_state_complete,
			(select ora3.done
			FROM %SCHEMA%.OpportunityRequiredArti ora3
			INNER JOIN %SCHEMA%.RequiredArtifacts ra3 ON ora3.requiredartifact = ra3.id
			where o.id = ora3.opportunity and ra3.name = 'Consumption Plan') as consumption_plan_complete,
			nvl(os.status, 'No Status Entered') as latest_status,
			to_char(os.creationdate, 'MM-DD-YYYY') as latest_status_date,
			os.lastupdatedby as latest_status_author
		FROM %SCHEMA%.Opportunity o
		INNER JOIN %SCHEMA%.Account a ON a.id = o.account
		LEFT OUTER JOIN %SCHEMA%.OpportunityTechHealth th ON th.opportunity = o.id
		LEFT OUTER JOIN %SCHEMA%.OpportunityWorkload w ON w.opportunity = o.id
		LEFT OUTER JOIN %SCHEMA%.User1 u ON o.createdby = u.useremail
		LEFT OUTER JOIN %SCHEMA%.OpportunityStatus os ON o.id = os.opportunity
		and not exists (select 1 FROM %SCHEMA%.OpportunityStatus os1 where os1.opportunity = o.id and os1.creationdate > os.creationdate)`

	var jsonResultTemplate = `{"ecal_workload_id":"%s","ecal_account_id":"%s","opportunity_id":"%s","workload_type":"%s","workload_identifier":"%s","account_name":"%s","cim_id":"%s","workload_summary":"%s","color":"%s","latest_ecal_stage_done": "%s","csa_executed":"%s","tech_lead":"%s","tech_manager":"%s","poc_required":"%s","poc_enddate":"%s","poc_status":"%s","poc_resolution":"%s","security_signoff":"%s","technical_signoff":"%s","cons_plan_signoff": "%s","cc_involved":"%s","cc_done":"%s","tech_blockers":"%s","commercial_blockers":"%s","covid_impact":"%s","ocs_engaged":"%s","expansion":"%s","tech_decider":"%s","tech_signoff_date":"%s","migration_by": "%s","tiger_se_email": "%s","partner_name":"%s","workload_progression":"%s","adopter_email":"%s","adopter_name":"%s","implementer_email":"%s","implementer_name":"%s","future_state_complete":"%s","current_state_complete":"%s","consumption_plan_complete":"%s","latest_status":"%s","latest_status_date":"%s","latest_status_author":"%s"},`

	// replace the %SCHEMA% template with the correct schema name
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])
	//fmt.Println(query)

	// run the query
	rows, err := DBPool.Query(query)
	if err != nil {
		thisError := fmt.Sprintf("[%s] [%s] getECALDataQuery: Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var ecalWorkloadID, ecalAccountID, opportunityID, workloadType, workloadIdentifier, accountName, cimID, workloadSummary, color, latestECALStageDone string
	var csaExecuted, techLead, techManager, pocRequired, pocEndDate, pocStatus, pocResolution, securitySignoff, technicalSignoff, consPlanSignoff string
	var ccInvolved, ccDone, techBlockers, commercialBlockers, covidImpact, ocsEngaged, expansion, techDecider, techSignoffDate, migrationBy, tigerSeEmail string
	var partnerName, workloadProgression, adopterEmail, adopterName, implementerEmail, implementerName, futureStateComplete, currentStateComplete, consumptionPlanComplete, latestStatus, latestStatusDate, latestStatusAuthor string

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&ecalWorkloadID, &ecalAccountID, &opportunityID, &workloadType, &workloadIdentifier, &accountName, &cimID, &workloadSummary, &color, &latestECALStageDone,
			&csaExecuted, &techLead, &techManager, &pocRequired, &pocEndDate, &pocStatus, &pocResolution, &securitySignoff, &technicalSignoff, &consPlanSignoff,
			&ccInvolved, &ccDone, &techBlockers, &commercialBlockers, &covidImpact, &ocsEngaged, &expansion, &techDecider, &techSignoffDate, &migrationBy, &tigerSeEmail,
			&partnerName, &workloadProgression, &adopterEmail, &adopterName, &implementerEmail, &implementerName, &futureStateComplete, &currentStateComplete, &consumptionPlanComplete, &latestStatus, &latestStatusDate, &latestStatusAuthor)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] getECALOpportunityQuery: Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, err.Error())
			return "", errors.New(thisError)
		}

		result += fmt.Sprintf(jsonResultTemplate,
			ecalWorkloadID, ecalAccountID, opportunityID, workloadType, workloadIdentifier, accountName, cimID, workloadSummary, color, latestECALStageDone,
			csaExecuted, techLead, techManager, pocRequired, pocEndDate, pocStatus, pocResolution, securitySignoff, technicalSignoff, consPlanSignoff,
			ccInvolved, ccDone, techBlockers, commercialBlockers, covidImpact, ocsEngaged, expansion, techDecider, techSignoffDate, migrationBy, tigerSeEmail,
			partnerName, workloadProgression, adopterEmail, adopterName, implementerEmail, implementerName, futureStateComplete, currentStateComplete, consumptionPlanComplete, latestStatus, latestStatusDate, latestStatusAuthor)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	//fmt.Printf("[%s] [%s] getECALDataQuery: results=%d\n", time.Now().Format(time.RFC3339), instanceEnv, count)
	return result, nil
}
