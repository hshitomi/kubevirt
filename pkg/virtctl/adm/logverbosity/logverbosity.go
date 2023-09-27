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
	noFlag = math.MaxUint - 1 // Default value if no flag is specified (dummy, we use cmd.Flags().Changed() to check if a flag is specified)
	noArg  = math.MaxUint     // Default value if no argument specified (e.g. "--virt-api" = "--virt-api=18446744073709551615")
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

// for receiving the flag argument
var verbosities [virtComponentNum]uint
var isReset bool

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

	cmd.Flags().UintVar(&verbosities[virtAPI], "virt-api", noFlag, "show/set virt-api log verbosity (0-9)")
	// Set the default value if the flag has no argument, because we use the flag without an argument (e.g. --virt-api) to show verbosity.
	// Otherwise, the pflag package will return an error due to missing argument.
	cmd.Flags().Lookup("virt-api").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&verbosities[virtController], "virt-controller", noFlag, "show/set virt-controller log verbosity (0-9)")
	cmd.Flags().Lookup("virt-controller").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&verbosities[virtHandler], "virt-handler", noFlag, "show/set virt-handler log verbosity (0-9)")
	cmd.Flags().Lookup("virt-handler").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&verbosities[virtLauncher], "virt-launcher", noFlag, "show/set virt-launcher log verbosity (0-9)")
	cmd.Flags().Lookup("virt-launcher").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&verbosities[virtOperator], "virt-operator", noFlag, "show/set virt-operator log verbosity (0-9)")
	cmd.Flags().Lookup("virt-operator").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().UintVar(&verbosities[all], "all", noFlag, "show/set all component log verbosity (0-9)")
	cmd.Flags().Lookup("all").NoOptDefVal = strconv.FormatUint(noArg, 10)

	cmd.Flags().BoolVar(&isReset, "reset", false, "reset log verbosity to the default verbosity (2) (empty the log verbosity)")

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

// virtComponent to component name
func getComponentNameByVirtComponent(component virtComponent) string {
	var virtComponentToComponentName = map[virtComponent]string{
		virtAPI:        "virt-api",
		virtController: "virt-controller",
		virtHandler:    "virt-handler",
		virtLauncher:   "virt-launcher",
		virtOperator:   "virt-operator",
		all:            "all",
	}
	return virtComponentToComponentName[component]
}

// virtComponent to JSON name
func getJSONNameByVirtComponent(component virtComponent) string {
	var virtComponentToJSONName = map[virtComponent]string{
		virtAPI:        "virtAPI",
		virtController: "virtController",
		virtHandler:    "virtHandler",
		virtLauncher:   "virtLauncher",
		virtOperator:   "virtOperator",
		all:            "all",
	}
	return virtComponentToJSONName[component]
}

// component name to JSON name
func getJSONNameByComponentName(componentName string) string {
	var componentNameToJSONName = map[string]string{
		"virt-api":        "virtAPI",
		"virt-controller": "virtController",
		"virt-handler":    "virtHandler",
		"virt-launcher":   "virtLauncher",
		"virt-operator":   "virtOperator",
		"all":             "all",
	}
	return componentNameToJSONName[componentName]
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

func hasVerbosityInKV(kv *v1.KubeVirt) (map[string]uint, error) {
	verbosityMap := map[string]uint{} // key: component name, value: verbosity
	// check the logVerbosity field in the KubeVirt CR
	lvJSON, err := json.Marshal(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity)
	if err != nil {
		return nil, err
	}
	// if there is a logVerbosity field, input the JSON data to the verbosityMap
	if err := json.Unmarshal(lvJSON, &verbosityMap); err != nil {
		return nil, err
	}
	return verbosityMap, nil
}

func createOutputMessage(verbosityVal map[string]uint, options map[string]uint) []string {
	var messages []string
	for component := virtAPI; component < all; component++ { // all is the last component, and do not need to check it
		componentName := getComponentNameByVirtComponent(component)
		JSONName := getJSONNameByVirtComponent(component)
		if _, exist := options["all"]; exist {
			messages = append(messages, fmt.Sprintf("%s=%d", componentName, verbosityVal[JSONName]))
		} else if _, exist := options[componentName]; exist {
			messages = append(messages, fmt.Sprintf("%s=%d", componentName, verbosityVal[JSONName]))
		}
	}
	return messages
}

func createShowMessage(kv *v1.KubeVirt, options map[string]uint) ([]string, error) {
	// set default verbosity first
	// it is used to fill the unattended verbosity with default verbosity
	// key: JSONName, value: verbosity
	var verbosityVal = map[string]uint{
		"virtAPI":        virtconfig.DefaultVirtAPILogVerbosity,
		"virtController": virtconfig.DefaultVirtControllerLogVerbosity,
		"virtHandler":    virtconfig.DefaultVirtHandlerLogVerbosity,
		"virtLauncher":   virtconfig.DefaultVirtLauncherLogVerbosity,
		"virtOperator":   virtconfig.DefaultVirtOperatorLogVerbosity,
	}

	// if verbosity has been set in the KubeVirt CR, use the verbosity
	lvMap, err := hasVerbosityInKV(kv)
	if err != nil {
		return nil, err
	}
	if len(lvMap) > 0 {
		for key, value := range lvMap {
			verbosityVal[key] = value
		}
	}

	// create a message to show verbosity for the specified component
	messages := createOutputMessage(verbosityVal, options)

	return messages, nil
}

func setVerbosity(lvMap map[string]uint, options map[string]uint, patchData *[]patch.PatchOperation, op *string, path *string) {
	// update lvMap based on the user-specified verbosity for all components
	if verbosity, exist := options["all"]; exist {
		for component := virtAPI; component < all; component++ {
			JSONName := getJSONNameByVirtComponent(component)
			lvMap[JSONName] = verbosity
		}
	}
	// update lvMap based on the user-specified verbosity for each component
	for componentName, verbosity := range options {
		if componentName == "all" {
			continue
		}
		JSONName := getJSONNameByComponentName(componentName)
		lvMap[JSONName] = verbosity
	}

	if len(lvMap) != 0 {
		addPatch(patchData, op, path, lvMap)
	}
}

func addPatch(patchData *[]patch.PatchOperation, op *string, path *string, lvMap map[string]uint) {
	*patchData = append(*patchData, patch.PatchOperation{
		Op:    *op,
		Path:  *path,
		Value: lvMap,
	})
}

func resetVerbosity(lvMap map[string]uint, patchData *[]patch.PatchOperation, op *string, path *string) {
	// reset only if verbosity exists, otherwise do nothing
	if len(lvMap) != 0 {
		// add an empty object (removing the logVerbosity field can be another method)
		emptyMap := map[string]uint{} // does not change the caller's lvMap
		addPatch(patchData, op, path, emptyMap)
	}
}

func createPatch(kv *v1.KubeVirt, options map[string]uint) ([]byte, error) {
	patchData := []patch.PatchOperation{}
	// just "add" is fine, no need of "replace" and "remove"
	// https://www.rfc-editor.org/rfc/rfc6902
	op := patch.PatchAddOp
	path := "/spec/configuration/developerConfiguration/logVerbosity"

	// if there is a logVerbosity field in the KubeVirt CR, fill in the data in the lvMap
	lvMap, err := hasVerbosityInKV(kv)
	if err != nil {
		return nil, err
	}
	if lvMap == nil {
		// if map is nil (logVerbosity field in the KubeVert CR is nil), need initialization
		lvMap = make(map[string]uint)
	}

	if isReset {
		resetVerbosity(lvMap, &patchData, &op, &path)
		lvMap = map[string]uint{}
	}

	// if the verbosity is specified for the component, update lvMap entry with the verbosity
	// if the verbosity is not specified for the component, and there is an existing verbosity in KubeVirt CR, use the existing verbosity
	// if we do not use the existing verbosity, the existing verbosity will be removed
	// if we use replace patch, it is possible to avoid removing the existing verbosity
	// (if components have exiting verbosity, use replace patch, if components do not have exiting verbosity, use add patch),
	// but we have to manage which components have the existing verbosity, which makes the code complicated
	setVerbosity(lvMap, options, &patchData, &op, &path)

	return json.Marshal(patchData)
}

func findOperation(cmd *cobra.Command, options map[string]uint) (operation, error) {
	isShow, isSet := false, false

	for component := virtAPI; component <= all; component++ {
		componentName := getComponentNameByVirtComponent(component)

		// check if the flag for the component is specified
		if !cmd.Flags().Changed(componentName) {
			continue // do nothing for the component
		}

		// if flag is specified, it means either set or show
		// if the value = noArg, it means show
		// if the value != noArg, it means set
		isShow = isShow || verbosities[component] == noArg
		isSet = isSet || verbosities[component] != noArg

		// check whether the verbosity is in the range
		if verbosities[component] != noArg && verbosities[component] > maxVerbosity {
			return nop, fmt.Errorf("%s: log verbosity must be %d-%d", componentName, minVerbosity, maxVerbosity)
		}

		// add a componentName and its verbosity to the map
		// in case of show: verbosity = noArg
		// in case of set: verbosity = 0-9
		// in case of reset: no information added to the map
		options[componentName] = verbosities[component]
	}

	// do not distinguish between set and reset at this point
	// because set and reset can coexist
	if isReset {
		isSet = true
	}

	if isShow && isSet {
		return nop, fmt.Errorf("only show or set is allowed")
	}

	if isShow {
		return show, nil
	} else if isSet {
		return set, nil
	}

	return nop, nil
}

func (c *Command) RunE(cmd *cobra.Command) error {
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

	// check the operation type (nop/show/set), and set the options map to use the map for show and set operations
	options := map[string]uint{} // key: component name, value: verbosity
	op, err := findOperation(cmd, options)
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
		messages, err := createShowMessage(kv, options)
		if err != nil {
			return err
		}
		for _, message := range messages {
			cmd.Println(message)
		}
	case set: // set and/or reset
		// create patch data
		patchData, err := createPatch(kv, options)
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
