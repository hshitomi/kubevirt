package logverbosity

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/spf13/cobra"

	"k8s.io/client-go/tools/clientcmd"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/apimachinery/patch"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
	"kubevirt.io/kubevirt/pkg/virtctl/templates"
)

type Command struct {
	clientConfig clientcmd.ClientConfig
	command      string
}

const (
	// for command parsing
	noFlag = math.MaxUint - 1 // Default value if the flag is not specified (flag to determine whether the flag is set or not)
	noArg  = math.MaxUint     // Default value if an argument is not specified (e.g. "--virt-api" = "--virt-api=18446744073709551615")
	// verbosity must be 0-9
	// https://kubernetes.io/docs/reference/kubectl/cheatsheet/#kubectl-output-verbosity-and-debugging
	minVerbosity = uint(0)
	maxVerbosity = uint(9)
)

// Log verbosity can be set per KubeVirt component
// https://kubevirt.io/user-guide/operations/debug/#setting-verbosity-per-kubevirt-component
// TODO: set verbosity per nodes
type virtComponent int

const (
	virtAPI virtComponent = iota // virtAPI must be at the first position because it is used for the iteration
	virtController
	virtHandler
	virtLauncher
	virtOperator
	all // all must be at the end, because it is used for the iteration
)

const virtComponentNum = int(all) + 1 // number of virt components

// to store the necessary information of each component
type virtComponentInfo struct {
	name      string // virt component name
	jsonName  string // JSON name
	verbosity uint   // log verbosity
	showFlag  bool   // true: if this component is a target of show operation
	setFlag   bool   // true: if this component is a target of set operation
}

// for receiving the flag argument
var (
	apiVerbosity        uint
	controllerVerbosity uint
	handlerVerbosity    uint
	launcherVerbosity   uint
	operatorVerbosity   uint
	allVerbosity        uint
	resetFlag           bool
)

// operation type of log-verbosity command
type operation int

const (
	show operation = iota
	set
	nop
)

func NewCommand(clientConfig clientcmd.ClientConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log-verbosity",
		Short: "Show or Set/Reset log verbosity. The verbosity must be 0-9. Show and Set/Reset cannot coexist.\n",
		Long: "- To show the log verbosity of one or more components " +
			"(when the log verbosity is unattended in the KubeVirt CR, show the default verbosity (2)).\n" +
			"- To set the log verbosity of one or more components.\n" +
			"- To reset the log verbosity of all components " +
			"(empty the log verbosity field, which means reset to the default verbosity (2)).\n\n" +
			"- The components are <virt-api|virt-controller|virt-handler|virt-launcher|virt-operator>.\n" +
			"- Show and Set/Reset cannot coexist.\n" +
			"- Verbosity must be 0-9.\n" +
			"- Flag syntax must be \"flag=arg\" (\"flag arg\" not supported).",
		Example: usage(),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := Command{command: "log-verbosity", clientConfig: clientConfig}
			return c.RunE(cmd)
		},
	}

	cmd.Flags().UintVar(&apiVerbosity, "virt-api", noFlag, "show/set virt-api log verbosity (0-9)")
	// Set the default value if the flag has no argument, because we use the flag without an argument (e.g. --virt-api) to show verbosity.
	// Otherwise, the pflag package will return an error due to missing argument.
	cmd.Flags().Lookup("virt-api").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&controllerVerbosity, "virt-controller", noFlag, "show/set virt-controller log verbosity (0-9)")
	cmd.Flags().Lookup("virt-controller").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&handlerVerbosity, "virt-handler", noFlag, "show/set virt-handler log verbosity (0-9)")
	cmd.Flags().Lookup("virt-handler").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&launcherVerbosity, "virt-launcher", noFlag, "show/set virt-launcher log verbosity (0-9)")
	cmd.Flags().Lookup("virt-launcher").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&operatorVerbosity, "virt-operator", noFlag, "show/set virt-operator log verbosity (0-9)")
	cmd.Flags().Lookup("virt-operator").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&allVerbosity, "all", noFlag, "show/set all component log verbosity (0-9)")
	cmd.Flags().Lookup("all").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().BoolVar(&resetFlag, "reset", false, "reset log verbosity to the default verbosity (2) (empty the log verbosity)")

	// cannot specify "reset" and "all" flag at the same time
	cmd.MarkFlagsMutuallyExclusive("reset", "all")

	cmd.SetUsageTemplate(templates.UsageTemplate())
	return cmd
}

// Command line flag syntax:
//
//	OK: --flag=x
//	NG: --flag x
//
// A flag without an argument should only be used for boolean flags.
// However, we want to use a flag without an argument (e.g. --virt-api) to show verbosity.
// To do this, we set the default value (NoOptDefVal) when the flag has no argument.
// In this case, we cannot use the "--flag x" syntax, because "--flag x" only applies to flags without a default value.
// See vendor/github.com/spf13/pflag/flag.go, especially note the order of the if clause below.
// https://github.com/kubevirt/kubevirt/blob/main/vendor/github.com/spf13/pflag/flag.go#L983C1-L998C3
func usage() string {
	usage := "  # reset (to default) log-verbosity for all components\n"
	usage += "  {{ProgramName}} adm logVerbosity --reset\n"

	usage += "  # show log-verbosity for all components:\n"
	usage += "  {{ProgramName}} adm log-verbosity --all\n"
	usage += "  # set log-verbosity to 3 for all components:\n"
	usage += "  {{ProgramName}} adm log-verbosity --all=3\n"

	usage += "  # show log-verbosity for virt-handler:\n"
	usage += "  {{ProgramName}} adm log-verbosity --virt-handler\n"
	usage += "  # set log-verbosity to 7 for virt-handler:\n"
	usage += "  {{ProgramName}} adm log-verbosity --virt-handler=7\n"

	usage += "  # show log-verbosity for virt-handler and virt-launcher\n"
	usage += "  {{ProgramName}} adm log-verbosity --virt-handler --virt-launcher\n"
	usage += "  # set log-verbosity for virt-handler to 7 and virt-launcher to 3\n"
	usage += "  {{ProgramName}} adm log-verbosity --virt-handler=7 --virt-launcher=3\n"

	usage += "  # reset all components to default besides virt-handler which is 7\n"
	usage += "  {{ProgramName}} adm log-verbosity --reset --virt-handler=7\n"
	usage += "  # set all components to 3 besides virt-handler which is 7\n"
	usage += "  {{ProgramName}} adm log-verbosity --all=3 --virt-handler=7\n"

	return usage
}

func detectInstallNamespaceAndName(virtClient kubecli.KubevirtClient) (namespace, name string, err error) {
	// listing KubeVirt CRs in all namespaces and see where it is installed
	kvs, err := virtClient.KubeVirt("").List(&k8smetav1.ListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("could not list KubeVirt CRs in all namespaces: %v", err)
	}
	if len(kvs.Items) == 0 {
		return "", "", errors.New("could not detect a KubeVirt installation")
	}
	if len(kvs.Items) > 1 {
		return "", "", errors.New("invalid kubevirt installation, more than one KubeVirt resource found")
	}
	namespace = kvs.Items[0].Namespace
	name = kvs.Items[0].Name
	return
}

func hasTarget(componentInfo *[virtComponentNum]*virtComponentInfo) (hasShowTarget, hasSetTarget bool) {
	var component virtComponent
	for component = virtAPI; component <= all; component++ {
		if !hasShowTarget && componentInfo[component].showFlag {
			hasShowTarget = true
		} else if !hasSetTarget && componentInfo[component].setFlag {
			hasSetTarget = true
		} else if hasShowTarget && hasSetTarget {
			// if at least one target of both show and set is found, return early (do not need to iterate through the end)
			return hasShowTarget, hasSetTarget
		}
	}
	return hasShowTarget, hasSetTarget
}

func findOperation(componentInfo *[virtComponentNum]*virtComponentInfo) (operation, error) {
	hasShowTarget, hasSetTarget := hasTarget(componentInfo) // true: if at least one component is the target of show/set operation
	if hasShowTarget && hasSetTarget {
		return -1, errors.New("show and set cannot coexist") // -1: dummy
	} else if hasShowTarget && resetFlag {
		return -1, errors.New("show and reset cannot coexist") // -1: dummy
	} else if hasShowTarget {
		return show, nil
	} else if hasSetTarget || resetFlag {
		return set, nil
	}
	return nop, nil // no flag specified
}

// check the logVerbosity field in the KubeVirt CR
// if there is a logVerbosity field, input the JSON data to the verbosityMap
func hasVerbosityInKV(kv *v1.KubeVirt) (map[string]uint, error) {
	verbosityMap := map[string]uint{} // key: name of virt component, value: verbosity
	lvJSON, err := json.Marshal(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(lvJSON, &verbosityMap); err != nil {
		return nil, err
	}
	return verbosityMap, nil
}

func createOutputMessage(verbosityVal *[virtComponentNum - 1]uint, componentInfo *[virtComponentNum]*virtComponentInfo) *string {
	var message string
	var component virtComponent
	for component = virtAPI; component < all; component++ { // all is the last component, and do not need to check it
		if componentInfo[component].showFlag || componentInfo[all].showFlag {
			// output format is like:
			// virt-api=1
			// virt-controller=2
			message += fmt.Sprintf("%s=%d\n", componentInfo[component].name, verbosityVal[component])
		}
	}
	return &message
}

func getComponentByJSON(jsonName string) virtComponent {
	var jsonToComponent = map[string]virtComponent{
		"virtAPI":        virtAPI,
		"virtController": virtController,
		"virtHandler":    virtHandler,
		"virtLauncher":   virtLauncher,
		"virtOperator":   virtOperator,
	}
	return jsonToComponent[jsonName]
}

func createShowMessage(kv *v1.KubeVirt, componentInfo *[virtComponentNum]*virtComponentInfo) (*string, error) {
	// set default verbosity first
	// it is used to fill the unattended verbosity with default verbosity
	verbosityVal := [virtComponentNum - 1]uint{
		uint(virtconfig.DefaultVirtAPILogVerbosity),
		uint(virtconfig.DefaultVirtControllerLogVerbosity),
		uint(virtconfig.DefaultVirtHandlerLogVerbosity),
		uint(virtconfig.DefaultVirtLauncherLogVerbosity),
		uint(virtconfig.DefaultVirtOperatorLogVerbosity),
	}

	// if verbosity has been set in the KubeVirt CR, use the verbosity
	lvMap, err := hasVerbosityInKV(kv)
	if err != nil {
		return nil, err
	}
	if len(lvMap) > 0 {
		for key, value := range lvMap {
			verbosityVal[getComponentByJSON(key)] = value
		}
	}

	// create a message to show verbosity for the specified component
	message := createOutputMessage(&verbosityVal, componentInfo)

	return message, nil
}

func setVerbosity(lvMap map[string]uint, componentInfo *[virtComponentNum]*virtComponentInfo) {
	var component virtComponent
	for component = virtAPI; component < all; component++ { // all is the last component, and do not need to check it
		var val uint
		val = math.MaxUint
		if componentInfo[component].setFlag {
			// set verbosity specified for this component
			val = componentInfo[component].verbosity
		} else if !componentInfo[component].setFlag && componentInfo[all].setFlag {
			// set verbosity specified for all components
			val = componentInfo[all].verbosity
		} else if !componentInfo[component].setFlag && !componentInfo[all].setFlag && !resetFlag {
			// verbosity not specified for this component
			// Use existing verbosity (in KubeVirt CR), if any.
			// Otherwise do nothing (no verbosity specified, and no existing verbosity in KubeVirt CR).
			if tempVal, exist := lvMap[componentInfo[component].jsonName]; exist {
				val = tempVal
			}
		}
		if val != math.MaxUint {
			lvMap[componentInfo[component].jsonName] = val
		}
	}
}

func addPatch(patchData *[]patch.PatchOperation, op *string, path *string, lvMap map[string]uint) {
	*patchData = append(*patchData, patch.PatchOperation{
		Op:    *op,
		Path:  *path,
		Value: lvMap,
	})
}

func createPatch(kv *v1.KubeVirt, componentInfo *[virtComponentNum]*virtComponentInfo) ([]byte, error) {
	patchData := []patch.PatchOperation{}

	// if there is a logVerbosity field in the KubeVirt CR, fill in the data in the lvMap
	lvMap, err := hasVerbosityInKV(kv)
	if err != nil {
		return nil, err
	}

	// "add" works well, no need of "replace"
	// https://www.rfc-editor.org/rfc/rfc6902
	op := patch.PatchAddOp
	path := "/spec/configuration/developerConfiguration/logVerbosity"

	// reset first, if reset flag is on
	if resetFlag {
		// reset only if verbosity exists
		if len(lvMap) != 0 {
			lvMap = map[string]uint{}               // reset existing verbosity
			addPatch(&patchData, &op, &path, lvMap) // add empty object, instead of removing logVerbosity field
		}
	}

	if lvMap == nil {
		// if map is nil (logVerbosity field in the KubeVert CR is nil), need initialization
		// otherwise key and value cannot be set in lvMap
		lvMap = make(map[string]uint)
	}

	// if the verbosity is specified for the component, update lvMap entry with the verbosity
	setVerbosity(lvMap, componentInfo)

	if len(lvMap) != 0 {
		addPatch(&patchData, &op, &path, lvMap)
	}

	return json.Marshal(patchData)
}

func setComponentInfo(cmd *cobra.Command, componentInfo *[virtComponentNum]*virtComponentInfo) error {
	componentEntries := [virtComponentNum]virtComponentInfo{
		{name: "virt-api", jsonName: "virtAPI", verbosity: apiVerbosity, showFlag: false, setFlag: false},
		{name: "virt-controller", jsonName: "virtController", verbosity: controllerVerbosity, showFlag: false, setFlag: false},
		{name: "virt-handler", jsonName: "virtHandler", verbosity: handlerVerbosity, showFlag: false, setFlag: false},
		{name: "virt-launcher", jsonName: "virtLauncher", verbosity: launcherVerbosity, showFlag: false, setFlag: false},
		{name: "virt-operator", jsonName: "virtOperator", verbosity: operatorVerbosity, showFlag: false, setFlag: false},
		{name: "all", jsonName: "", verbosity: allVerbosity, showFlag: false, setFlag: false},
	}

	var component virtComponent
	for component = virtAPI; component <= all; component++ {
		// check for the existence of flag and argument, and set hasFlag and hasArg
		// also check for the verbosity in the range (0-9)
		var hasFlag = false
		var hasArg = false
		if cmd.Flags().Changed(componentEntries[component].name) {
			hasFlag = true
			if componentEntries[component].verbosity != noArg {
				hasArg = true
				if componentEntries[component].verbosity > maxVerbosity && componentEntries[component].verbosity < noArg {
					// verbosity out of range
					return fmt.Errorf("%s: log verbosity must be %d-%d", componentEntries[component].name, minVerbosity, maxVerbosity)
				}
			}
		}

		// set showFlag and setFlag for the component based on the following criteria
		// show : hasFlag==true and hasArg==false
		// set : hasFlag==true and hasArg==true
		if hasFlag && !hasArg { // show
			componentEntries[component].showFlag = true
		} else if hasFlag && hasArg { // set
			componentEntries[component].setFlag = true
		}

		// set each componentInfo
		componentInfo[component] = &componentEntries[component]
	}

	return nil
}

func (c *Command) RunE(cmd *cobra.Command) error {
	var componentInfo [virtComponentNum]*virtComponentInfo // stores the information needed by each component to execute the command

	// set componentInfo
	// after this function, the componentInfo will never be changed
	err := setComponentInfo(cmd, &componentInfo)
	if err != nil {
		return err
	}

	// get client
	virtClient, err := kubecli.GetKubevirtClientFromClientConfig(c.clientConfig)
	if err != nil {
		return err
	}
	// get install namespace and name
	namespace, name, err := detectInstallNamespaceAndName(virtClient)
	if err != nil {
		return err
	}
	// get KubeVirt CR
	kv, err := virtClient.KubeVirt(namespace).Get(name, &k8smetav1.GetOptions{})
	if err != nil {
		return err
	}

	// check the operation type (nop/show/set)
	op, err := findOperation(&componentInfo)
	if err != nil {
		return err
	}

	switch op {
	case nop:
		err := cmd.Help()
		if err != nil {
			return err
		}
		return errors.New("no flag specified - expecting at least one flag")
	case show:
		message, err := createShowMessage(kv, &componentInfo)
		if err != nil {
			return err
		}
		cmd.Println(*message)
	case set:
		// create patch data
		patchData, err := createPatch(kv, &componentInfo)
		if err != nil {
			return err
		}
		// apply patch, if patch data exists
		if len(patchData) != 0 {
			_, err = virtClient.KubeVirt(namespace).Patch(name, types.JSONPatchType, patchData, &k8smetav1.PatchOptions{})
			if err != nil {
				return err
			}
		}
		cmd.Println("successfully set/reset the log verbosity")
	default:
		return fmt.Errorf("op: an unknown operation: %v", op)
	}

	return nil
}
