package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	pd "github.com/PagerDuty/go-pagerduty"
	"github.com/aws/aws-sdk-go/service/cloudtrail"
	"github.com/openshift-online/ocm-cli/pkg/dump"
	"github.com/openshift/osdctl/cmd/servicelog"
	sl "github.com/openshift/osdctl/internal/servicelog"
	"github.com/openshift/osdctl/pkg/config"
	"github.com/openshift/osdctl/pkg/osdCloud"
	"github.com/openshift/osdctl/pkg/printer"
	"github.com/openshift/osdctl/pkg/utils"
	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type contextOptions struct {
	output     string
	verbose    bool
	full       bool
	clusterID  string
	baseDomain string
	days       int
	oauthtoken string
	externalID string
	infraID    string
	awsProfile string
}

// newCmdContext implements the context command to show the current context of a cluster
func newCmdContext() *cobra.Command {
	ops := newContextOptions()
	contextCmd := &cobra.Command{
		Use:               "context",
		Short:             "Shows the context of a specified cluster",
		Args:              cobra.ExactArgs(1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(ops.complete(cmd, args))
			cmdutil.CheckErr(ops.run())
		},
	}

	contextCmd.Flags().StringVarP(&ops.clusterID, "cluster-id", "C", "", "Cluster ID")
	contextCmd.Flags().StringVarP(&ops.awsProfile, "profile", "p", "", "AWS Profile")
	contextCmd.Flags().BoolVarP(&ops.verbose, "verbose", "", false, "Verbose output")
	contextCmd.Flags().BoolVarP(&ops.full, "full", "", false, "Run full suite of checks.")
	contextCmd.Flags().IntVarP(&ops.days, "days", "z", 30, "Command will display X days of Error SLs sent to the cluster. Days is set to 30 by default")
	contextCmd.Flags().StringVarP(&ops.oauthtoken, "oauthtoken", "t", "", "Pass in PD oauthtoken directly. If not passed in, by default will read token from ~/.config/pagerduty-cli/config.json")

	return contextCmd
}

func newContextOptions() *contextOptions {
	return &contextOptions{}
}

func (o *contextOptions) complete(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "Provide exactly one cluster ID")
	}

	if o.days < 1 {
		return fmt.Errorf("Cannot have a days value lower than 1")
	}

	// Create OCM client to talk to cluster API
	ocmClient := utils.CreateConnection()
	defer func() {
		if err := ocmClient.Close(); err != nil {
			fmt.Printf("Cannot close the ocmClient (possible memory leak): %q", err)
		}
	}()

	clusters := utils.GetClusters(ocmClient, args)
	if len(clusters) != 1 {
		return fmt.Errorf("unexpected number of clusters matched input. Expected 1 got %d", len(clusters))
	}
	o.clusterID = clusters[0].ID()
	o.baseDomain = clusters[0].DNS().BaseDomain()
	o.externalID = clusters[0].ExternalID()
	o.infraID = clusters[0].InfraID()

	return nil
}

func (o *contextOptions) run() error {

	connection := utils.CreateConnection()
	defer connection.Close()

	err := printClusterInfo(o.clusterID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't print cluster info: %v\n", err)
		os.Exit(1)
	}

	limitedSupportReasons, err := utils.GetClusterLimitedSupportReasons(connection, o.clusterID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't retrieve cluster limited support reasons: %v\n", err)
		os.Exit(1)
	}

	// Check support status of cluster
	err = printSupportStatus(limitedSupportReasons)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't print support status: %v\n", err)
		os.Exit(1)
	}

	// Print the Servicelogs for this cluster
	err = o.printServiceLogs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't print service logs: %v\n", err)
		os.Exit(1)
	}

	// Print all triggered and acknowledged pd alerts
	err = o.printPDAlerts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't print pagerduty alerts: %v\n", err)
		os.Exit(1)
	}

	// Print other helpful links
	err = o.printOtherLinks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't print other links: %v\n", err)
	}

	if o.full {
		err = o.printCloudTrailLogs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Can't print cloudtrail: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println()
		fmt.Println("============================================================")
		fmt.Println("CloudTrail events for the Cluster")
		fmt.Println("============================================================")
		println("Not polling cloudtrail logs, use --full flag to do so (must be logged into the correct hive to work).")
	}
	return nil
}

func printClusterInfo(clusterID string) error {

	fmt.Println("============================================================")
	fmt.Println("Cluster Info")
	fmt.Println("============================================================")

	cmd := "ocm describe cluster " + clusterID
	output, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		fmt.Println(string(output))
		fmt.Print(err)
		return err
	}
	fmt.Println(string(output))

	return nil
}

// printSupportStatus reports if a cluster is in limited support or fully supported.
func printSupportStatus(limitedSupportReasons []*utils.LimitedSupportReasonItem) error {

	fmt.Println("============================================================")
	fmt.Println("Limited Support Status")
	fmt.Println("============================================================")

	// No reasons found, cluster is fully supported
	if len(limitedSupportReasons) == 0 {
		fmt.Printf("Cluster is fully supported\n")
		fmt.Println()
		return nil
	}

	table := printer.NewTablePrinter(os.Stdout, 20, 1, 3, ' ')
	table.AddRow([]string{"Reason ID", "Summary", "Details"})
	for _, clusterLimitedSupportReason := range limitedSupportReasons {
		table.AddRow([]string{clusterLimitedSupportReason.ID, clusterLimitedSupportReason.Summary, clusterLimitedSupportReason.Details})
	}
	// Add empty row for readability
	table.AddRow([]string{})
	table.Flush()

	return nil
}

func (o *contextOptions) printServiceLogs() error {

	// Get the SLs for the cluster
	slResponse, err := servicelog.FetchServiceLogs(o.clusterID)
	if err != nil {
		return err
	}

	var serviceLogs sl.ServiceLogShortList
	err = json.Unmarshal(slResponse.Bytes(), &serviceLogs)
	if err != nil {
		fmt.Printf("Failed to unmarshal the SL response %q\n", err)
		return err
	}

	// Parsing the relevant servicelogs
	// - We only care about SLs sent in the past 'o.days' days
	var errorServiceLogs []sl.ServiceLogShort
	for _, serviceLog := range serviceLogs.Items {
		// If the days since the SL was sent exceeds o.days days, we're not interested
		if (time.Since(serviceLog.CreatedAt).Hours() / 24) > float64(o.days) {
			continue
		}

		errorServiceLogs = append(errorServiceLogs, serviceLog)
	}

	fmt.Println("============================================================")
	fmt.Println("Service Logs sent in the past", o.days, "Days")
	fmt.Println("============================================================")

	if o.verbose {
		marshalledSLs, err := json.MarshalIndent(errorServiceLogs, "", "  ")
		if err != nil {
			return err
		}
		dump.Pretty(os.Stdout, marshalledSLs)
	} else {
		// Non verbose only prints the summaries
		for i, errorServiceLog := range errorServiceLogs {
			fmt.Printf("%d. %s (%s)\n", i, errorServiceLog.Summary, errorServiceLog.CreatedAt.Format(time.RFC3339))
		}
	}
	fmt.Println()

	return nil
}

func (o *contextOptions) printPDAlerts() error {
	var oauthtoken string
	if o.oauthtoken != "" {
		oauthtoken = o.oauthtoken
	} else {
		pdConfig := config.LoadPDConfig("/.config/pagerduty-cli/config.json")
		if len(pdConfig.MySubdomain) == 0 {
			return fmt.Errorf("unable to parse PagerDuty config")
		}
		if len(pdConfig.MySubdomain[0].AccessToken) == 0 {
			return fmt.Errorf("unable to locate oauth accesstoken in PagerDuty config")
		}
		oauthtoken = pdConfig.MySubdomain[0].AccessToken
	}
	client := pd.NewOAuthClient(oauthtoken)

	ctx := context.TODO()
	lsResponse, err := client.ListServicesWithContext(ctx, pd.ListServiceOptions{Query: o.baseDomain})

	if err != nil {
		fmt.Printf("Failed to ListServicesWithContext %q\n", err)
		return err
	}

	if len(lsResponse.Services) != 1 {
		return fmt.Errorf("unexpected number of services matched input. Expected 1 got %d", len(lsResponse.Services))
	}

	serviceID := lsResponse.Services[0].ID
	liResponse, err := client.ListIncidentsWithContext(
		ctx,
		pd.ListIncidentsOptions{
			ServiceIDs: []string{serviceID},
			Statuses:   []string{"triggered", "acknowledged"},
		},
	)
	if err != nil {
		fmt.Printf("Failed to ListIncidentsWithContext %q\n", err)
		return err
	}

	fmt.Println("============================================================")
	fmt.Println("Pagerduty alerts for the Cluster")
	fmt.Println("============================================================")
	fmt.Printf("Link to PD Service: https://redhat.pagerduty.com/service-directory/%s\n", serviceID)
	table := printer.NewTablePrinter(os.Stdout, 20, 1, 3, ' ')
	table.AddRow([]string{"Urgency", "Title", "Created At"})
	for _, incident := range liResponse.Incidents {
		table.AddRow([]string{incident.Urgency, incident.Title, incident.CreatedAt})
	}
	// Add empty row for readability
	table.AddRow([]string{})
	err = table.Flush()
	if err != nil {
		fmt.Println("error while flushing table: ", err.Error())
		return err
	}

	return err
}

func (o *contextOptions) printOtherLinks() error {
	fmt.Println("============================================================")
	fmt.Println("Splunk audit logs for the Cluster (set the time in Splunk)")
	fmt.Println("============================================================")
	fmt.Printf("Link to Splunk audit logs: https://osdsecuritylogs.splunkcloud.com/en-US/app/search/search?q=search%%20index%%3D%%22openshift_managed_audit%%22%%20clusterid%%3D%%22%s%%22\n\n", o.infraID)

	fmt.Println("============================================================")
	fmt.Println("OHSS tickets for the Cluster")
	fmt.Println("============================================================")
	fmt.Printf("Link to OHSS tickets: https://issues.redhat.com/issues/?jql=project%%20%%3D%%20OHSS%%20and%%20(%%22Cluster%%20ID%%22%%20~%%20%%20%%22%s%%22%%20OR%%20%%22Cluster%%20ID%%22%%20~%%20%%22%s%%22)\n\n", o.clusterID, o.externalID)

	return nil
}

func (o *contextOptions) printCloudTrailLogs() error {

	awsJumpClient, err := osdCloud.GenerateAWSClientForCluster(o.awsProfile, o.clusterID)
	if err != nil {
		return err
	}

	foundEvents := []*cloudtrail.Event{}
	var eventSearchInput = cloudtrail.LookupEventsInput{}

	println("Pulling and filtering the past 40 pages of Cloudtrail data")
	for counter := 0; counter <= 40; counter++ {
		print(".")
		cloudTrailEvents, err := awsJumpClient.LookupEvents(&eventSearchInput)
		if err != nil {
			return err
		}

		foundEvents = append(foundEvents, cloudTrailEvents.Events...)

		// for pagination
		eventSearchInput.NextToken = cloudTrailEvents.NextToken
		if cloudTrailEvents.NextToken == nil {
			break
		}
	}
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("CloudTrail events for the Cluster")
	fmt.Println("============================================================")

	table := printer.NewTablePrinter(os.Stdout, 20, 1, 3, ' ')
	table.AddRow([]string{"EventId", "EventName", "Username", "EventTime"})
	for _, event := range foundEvents {
		if strings.Contains(*event.EventName, "Get") || strings.Contains(*event.EventName, "List") || strings.Contains(*event.EventName, "Describe") || strings.Contains(*event.EventName, "AssumeRole") {
			continue
		}
		if event.Username == nil {
			table.AddRow([]string{*event.EventId, *event.EventName, "", event.EventTime.String()})
		} else {
			if strings.Contains(*event.Username, "RH-SRE-") {
				continue
			}
			table.AddRow([]string{*event.EventId, *event.EventName, *event.Username, event.EventTime.String()})
		}

	}
	// Add empty row for readability
	table.AddRow([]string{})
	err = table.Flush()
	if err != nil {
		fmt.Println("error while flushing table: ", err.Error())
		return err
	}

	return nil
}
