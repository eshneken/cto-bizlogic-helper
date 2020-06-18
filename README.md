# CTO Bizlogic Helper
The purpose of this module is to serve as an external helper for any business logic for the suite of Cloud Technology Office (CTO) applications that are better served living outside the core VBCS Application.  This module is currently configured to support direct database access for STS & ECAL.

The app requires a file named *config.json* to be present the same directory from which the app is run.  A sample file (with identifying credentials removed) looks like this:

```json
{
    "ServiceListenPort": "{{HTTP listen port for this instance}}",
    "ServiceUsername": "{{basic_auth_username_for_this_service}}",
    "ServicePassword": "{{basic_auth_password_for_this_service}}",
    "DBConnectString": "admin/{{password}}@{{DB SID}}",
    "IdentityFilename": "identities.json",
    "IdentityMgrLeads": "mgr.1@email.com,mgr.2@email.com",
    "InstanceEnvironments": "ecal-dev-preview,ecal-dev-stage,sts-dev-preview,sts-dev-stage",
    "ECALOpportunitySyncTarget": "ecal-dev-preview",
    "SchemaNames": "{{dev-preview schema name}},{dev-stage schema name}},{prod-stage schema name}},{prod-live schema name}}",
    "ECALManagerHierarchyQuery": "SELECT UserEmail FROM %SCHEMA%.user1 u INNER JOIN %SCHEMA%.roletype rt ON u.rolename = rt.id WHERE rt.rolename = 'Manager' START WITH useremail = :1 CONNECT BY PRIOR useremail = manager",
    "STSManagerHierarchyQuery": "SELECT UserEmail FROM %SCHEMA%.STSUser u INNER JOIN %SCHEMA%.STSRole r ON u.rolename = r.id WHERE r.rolename = 'Manager' START WITH useremail = :1 CONNECT BY PRIOR useremail = manager"    
}
```
```
When used with the OCI Secrets Service the format of any vaulted credentials must be in the form of:  
```
[vault]FieldName:SecretOCID
```

For example:
``` 
"DBConnectString": "[vault]DBConnectString:ocid1.vaultsecret.oc1.iad.amaaaaaabxdvnfaaojh62dolelcp4xk93xrms6jfagdec2p3slzs7fx2iicq"
```

Note that an instance of this service must run in each compartment (e.g. one instance for the DEV compartment and one for PROD).  The InstanceEnvironments example shown above is for the DEV compartment, the PROD compartment whould have a different set of tokens.

This utility runs as an http server on a compute instance.  It listens, by default, on port 80 and requires the appropriate linux and cloud firewall/security list rules to allow incoming traffic to be created.  

It is expected that a HashiCorp Vault server is accesible at startup time to decode any secrets from the *config.json* file (those entries are in the form [vault]$Key).  If you're running locally and your *config.json* does not have any secrets, you can skip the vault integration by passing the --novault flag to the startup.

## Building the service from code
The following steps can be followed to build this service on Oracle Cloud Infrastructure (OCI):
1. Create a VCN with all related resources and update default security list to allow ingress access for TCP/80 and TCP/443
1. Create compute instance from "Oracle Developer" marketplace image
1. SSH into instance and open ingress for TCP/80 in linux firewall
    1. sudo firewall-cmd --zone=public --add-port=80/tcp --permanent
    1. sudo firewall-cmd --reload
1. Set-up GOPATH
    1. export GOPATH=/usr/share/gocode
    1. echo "export GOPATH=/usr/share/gocode" >> ~/.bash_profile
    1. sudo chmod -R 777 $GOPATH
1. Clone git repo (git clone {{this repo name}})
    1. git clone https://github.com/eshneken/cto-bizlogic-helper
1. Download gjson dependency package 
    1. go get -u github.com/tidwall/gjson
1. Download godror dependency package 
    1. go get -u github.com/godror/godror
1. Download OCI golang sdk dependency package
    1. go get -u github.com/oracle/oci-go-sdk
1. Upload the ATP wallet file to the instance, copy to ~/wallet, and unzip the contents into that folder
    1. scp wallet.zip opc@{{ip_addr}}:/home/opc; [LOCAL]
    1. mkdir wallet; cd wallet; unzip ../wallet.zip; cd ..; rm wallet.zip
1. Configure TNS settings
    1. vi /home/opc/wallet/sqlnet.ora and replace ?/network/admin/ with /home/opc/wallet/
    1. vi ~/.bash_profile and add 'export TNS_ADMIN=/home/opc/wallet' to the bottom
    1. source ~/.bash_profile
1. Add a config.json file to the cto-ecal-bizlogic directory with the appropriate values
1. Add a dummy identities.json file.  This file is necessary in all environments but will be actively updated in the prod environment.  However, its existence drives the health check process so create it so that it exists at startup.
    1. touch identities.json
1. Build the package
    1. go get
    1. go build
1. Allow the service to bind to port 80
    1. sudo setcap CAP_NET_BIND_SERVICE=+eip /home/opc/cto-bizlogic-helper/cto-bizlogic-helper
1. Configure the service to run automatically on startup/reboot.  This will also start the service now.
    1. chmod ug+x ./startServer.sh
    1. sudo vi /etc/systemd/system/cto-bizlogic-helper.service
        1. paste the contents of startup_service_config.txt into the editor window and save/quit.
    1. sudo systemctl daemon-reload
    1. sudo systemctl enable cto-bizlogic-helper.service
    1. sudo systemctl start cto-bizlogic-helper.service
1. Verify correct startup by running *cat ~/server.out*.  You should see correct startup messages with no errors or panics.  You can also execute *curl localhost/health* and make sure **HEALTH_OK** is returned.

## Updating the service from code
If the service code is updated and you need to rebuild/re-rerun then follow these following steps
1. Find the PID of the running service and kill
    1. ps -ef |grep cto-bizlogic
    1. sudo kill %PID%
1. Get the latest code
    1. cd ~/cto-bizlogic-helper
    1. git pull
    1. sudo go build
1. Update config.json if necessary
1. Start the service
    1. ./startServer.sh

## Usage
All endpoints require basic auth username & password except for the health check

* health:                           http://{{hostname}}/health [GET]
* getManagerQuery:                  http://{{hostname}}/getManagerQuery?managerEmail={{email_addr}}&instanceEnvironment={{instance-env}} [GET]
* getSTSManagerDashboardSummary:    http://{{hostname}}/getSTSManagerDashboardSummary?managerEmail={{email_addr}}&instanceEnvironment={{instance-env}} [GET]
* getIdentities:                    http://{{hostname}}/getIdentities [GET]
* postIdentities:                   http://{{hostname}}/postIdentities [POST]
* postReferenceData:                http://{{hostname}}/postOpportunityLookup?position={{first|middle|last|reprocess}}&type={{identity|opportunity}} [POST]

In production mode the server should always work with HashiCorp Vault.  In development mode, a --novault argument may be passed
in on the command line when starting the server to disable checking Vault and reading config values at face value.

## Principles for API Usage
* OIC REST API Reference:  https://docs.oracle.com/en/cloud/paas/identity-cloud/rest-api/
* Working with VBCS Business Object APIs:  https://docs.oracle.com/en/cloud/paas/app-builder-cloud/consume-rest/index.html

## Third Party Packages Used

 * Read-only JSON pathing support:  https://github.com/tidwall/gjson
 * Oracle Database access:  https://github.com/godror/godror
