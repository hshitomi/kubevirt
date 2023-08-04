/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */

package logverbosity

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/tools/clientcmd"

	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/virtctl/templates"
)

const (
	LOG_VERBOSITY = "log-verbosity"
)

type Command struct {
	clientConfig clientcmd.ClientConfig
	command      string
}

var (
	virtAPIFlag        bool
	virtControllerFlag bool
	virtHanderFlag     bool
	virtLauncherFlag   bool
	virtOperatorFlag   bool
	allFlag            bool
	resetFlag          bool
)

func NewCommand(clientConfig clientcmd.ClientConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log-verbosity",
		Short:   "Set/Reset log verbosity level",
		Long:    "Set log verbosity level to one or more compoents (virt-api/virt-controller/virt-handler/virt-llauncher/virt-operator). Reset the log verbosity level to all components.",
		Example: usage(),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := Command{command: LOG_VERBOSITY, clientConfig: clientConfig}
			return c.RunE(args)
		},
	}

	cmd.Flags().BoolVar(&virtAPIFlag, "virt-api", virtAPIFlag, "Specify virt-api log verbosity level.")
	cmd.Flags().BoolVar(&virtControllerFlag, "virt-controller", virtControllerFlag, "Specify virt-controller log verbosity level.")
	cmd.Flags().BoolVar(&virtHanderFlag, "virt-handler", virtHanderFlag, "Specify virt-handler log verbosity level.")
	cmd.Flags().BoolVar(&virtLauncherFlag, "virt-launcher", virtLauncherFlag, "Specify virt-launcher log verbosity level.")
	cmd.Flags().BoolVar(&virtOperatorFlag, "virt-operator", virtOperatorFlag, "Specify virt-operator log verbosity level.")
	cmd.Flags().BoolVar(&allFlag, "all", allFlag, "Set all component log verbosity levels.")
	cmd.Flags().BoolVar(&resetFlag, "reset", resetFlag, "Reset log verbosity level.")

	// no required flag

	cmd.SetUsageTemplate(templates.UsageTemplate())
	return cmd
}

func usage() string {
	usage := "  # Set a log verbosity level to a component:\n"
	usage += "  {{ProgramName}} adm logVerbosity 3 --virt-api"
	usage += "  # Set a log verbosity level to multiple components:\n"
	usage += "  {{ProgramName}} adm logVerbosity 3 --virt-api  --virt-handler"
	usage += "  # Set a log verbosity level to all components:\n"
	usage += "  {{ProgramName}} adm logVerbosity 3 --all"
	usage += "  # Reset the log verbosity level to all components:\n"
	usage += "  {{ProgramName}} adm logVerbosity --reset"

	return usage
}

func (c *Command) RunE(args []string) error {
	/*
	 * flag strength: reset > all > component
	 */

	var logLevel uint

	// parse argument
	if !resetFlag {
		if len(args) == 0 {
			return fmt.Errorf("expecting log verbosity level")
		}
		level, err := strconv.ParseUint(args[0], 10, 0)
		if err != nil {
			return err
		}
		logLevel = uint(level)

		/*
		 * user can specify logLevel 0
		 * if we do not allow user to specify logLevel 0, need error handling here.
		 */

	}

	// adm command always sees the KubeVirt resource of namespace="kubevirt", name="kubevirt"
	const (
		NAMESPACE = "kubevirt"
		NAME      = "kubevirt"
	)

	virtClient, err := kubecli.GetKubevirtClientFromClientConfig(c.clientConfig)
	if err != nil {
		return fmt.Errorf("cannot obtain KubeVirt client: %v", err)
	}

	// get KubeVirt object
	kv, err := virtClient.KubeVirt(NAMESPACE).Get(NAME, &k8smetav1.GetOptions{})
	if err != nil {
		return err
	}

	// create patch
	var patch string

	// get LogVerbosity strcut entries
	lv := kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity

	if lv == nil {
		// no LogVerbosity etnry yet

		if resetFlag {

			// do nothing, because there exist no LogVerbosity setting
			return nil

		} else if allFlag {

			patch = fmt.Sprintf(`
					[{"op": "add","path": "/spec/configuration/developerConfiguration/logVerbosity","value": {"virtHandler": %d, "virtAPI": %d, "virtController": %d, "virtLauncher": %d, "virtOperator": %d}}]
				`, logLevel, logLevel, logLevel, logLevel, logLevel)

		} else if virtAPIFlag || virtControllerFlag || virtHanderFlag || virtLauncherFlag || virtOperatorFlag {

			var components []string

			if virtAPIFlag {
				components = append(components, "virtAPI")
			}
			if virtControllerFlag {
				components = append(components, "virtController")
			}
			if virtHanderFlag {
				components = append(components, "virtHandler")
			}
			if virtLauncherFlag {
				components = append(components, "virtLauncher")
			}
			if virtOperatorFlag {
				components = append(components, "virtOperator")
			}

			// components never nil

			patch = fmt.Sprintf(`[{"op": "add","path": "/spec/configuration/developerConfiguration/logVerbosity","value": {`)
			for i, component := range components {
				if i == (len(components) - 1) {
					patch += fmt.Sprintf(`"%s": %d`, component, logLevel)
				} else {
					patch += fmt.Sprintf(`"%s": %d,`, component, logLevel)
				}
			}
			patch += fmt.Sprintf(`}}]`)

		}

	} else {
		// only change the value for the specifed component(s)
		// for other components, use existing value
		apiVal := lv.VirtAPI
		controllerVal := lv.VirtController
		handlerVal := lv.VirtHandler
		launcherVal := lv.VirtLauncher
		operatorVal := lv.VirtOperator

		if resetFlag {

			/*
				 Do we need to remove one entry by one entry? So that "LogVerbosity" entry remains.
				 	patch := `[
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity/virtAPI"},
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity/virtController"},
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity/virtHandler"},
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity/virtLauncher"},
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity/virtOperator"}
					]`
			*/

			// remove "logVerbosity"
			patch = `[{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity"}]`

		} else if allFlag {
			/*
			 * reset settings and add settings for all components
			 * Or, do we need to replace existing level with the new level, and also add the new level for the not specified entry?
			 */
			patch = fmt.Sprintf(`
					[
						{"op": "remove","path": "/spec/configuration/developerConfiguration/logVerbosity"},
						{"op": "add","path": "/spec/configuration/developerConfiguration/logVerbosity","value": {"virtHandler": %d, "virtAPI": %d, "virtController": %d, "virtLauncher": %d, "virtOperator": %d}}
					]
				`, logLevel, logLevel, logLevel, logLevel, logLevel)

		} else if virtAPIFlag || virtControllerFlag || virtHanderFlag || virtLauncherFlag || virtOperatorFlag {

			patchEntry := map[string]uint{}

			// no existing entry (nil value = 0)

			if virtAPIFlag {
				patchEntry["virtAPI"] = logLevel
			} else {
				if apiVal != 0 {
					patchEntry["virtAPI"] = apiVal
				}
			}
			if virtControllerFlag {
				patchEntry["virtController"] = logLevel
			} else {
				if controllerVal != 0 {
					patchEntry["virtController"] = controllerVal
				}
			}
			if virtHanderFlag {
				patchEntry["virtHandler"] = logLevel
			} else {
				if handlerVal != 0 {
					patchEntry["virtHandler"] = handlerVal
				}
			}
			if virtLauncherFlag {
				patchEntry["virtLauncher"] = logLevel
			} else {
				if launcherVal != 0 {
					patchEntry["virtLauncher"] = launcherVal
				}
			}
			if virtOperatorFlag {
				patchEntry["virtOperator"] = logLevel
			} else {
				if operatorVal != 0 {
					patchEntry["virtOperator"] = operatorVal
				}
			}

			// patchEntry never nil

			patch = fmt.Sprintf(`[{"op": "replace","path": "/spec/configuration/developerConfiguration/logVerbosity","value": {`)

			i := 0
			for key, value := range patchEntry {
				if i == (len(patchEntry) - 1) {
					patch += fmt.Sprintf(`"%s": %d`, key, value)
				} else {
					patch += fmt.Sprintf(`"%s": %d,`, key, value)
				}
				i++
			}
			patch += fmt.Sprintf(`}}]`)

		}

	}

	// apply patch
	if patch != "" {
		_, err = virtClient.KubeVirt(NAMESPACE).Patch(NAME, types.JSONPatchType, []byte(patch), &k8smetav1.PatchOptions{})
		if err != nil {
			panic(err)
		}
	} else {
		// patch = "" means no flag valid specified
		return fmt.Errorf("Invalid command format. Need one or more flag.")
	}

	fmt.Printf("Succesfully set/reset the log verbosity level.\n")
	return nil
}
