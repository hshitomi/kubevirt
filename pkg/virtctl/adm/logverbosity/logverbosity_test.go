package logverbosity_test

import (
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	"kubevirt.io/kubevirt/tests/clientcmd"

	"kubevirt.io/client-go/kubecli"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
)

const (
	NAMESPACE = "kubevirt"
	NAME      = "kubevirt"
)

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

var _ = Describe("Log Verbosity", func() {

	var ctrl *gomock.Controller
	var kvInterface *kubecli.MockKubeVirtInterface

	var kv *v1.KubeVirt
	var kvs *v1.KubeVirtList

	BeforeEach(func() {

		// create mock KubeVirt CR
		kv = NewKubeVirtWithoutVerbosity(NAMESPACE, NAME)
		kvs = kubecli.NewKubeVirtList(*kv)

		// create the wrapper that would return the mock virt client to the code being unit tested
		ctrl = gomock.NewController(GinkgoT())
		kubecli.GetKubevirtClientFromClientConfig = kubecli.GetMockKubevirtClientFromClientConfig
		kubecli.MockKubevirtClientInstance = kubecli.NewMockKubevirtClient(ctrl)

		// create mock interface (clientset)
		kvInterface = kubecli.NewMockKubeVirtInterface(ctrl)

		// set up mock client bahavior
		kubecli.MockKubevirtClientInstance.EXPECT().KubeVirt(NAMESPACE).Return(kvInterface).AnyTimes()
		kubecli.MockKubevirtClientInstance.EXPECT().KubeVirt("").Return(kvInterface).AnyTimes()

		// set up mock interface behavior
		kvInterface.EXPECT().Get(NAME, gomock.Any()).Return(kv, nil).AnyTimes()
		kvInterface.EXPECT().List(gomock.Any()).Return(kvs, nil).AnyTimes()
		kvInterface.EXPECT().Patch(NAME, types.JSONPatchType, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ any, _ any, patchData []byte, _ any, _ ...any) (*v1.KubeVirt, error) {
				patch, err := jsonpatch.DecodePatch(patchData)
				Expect(err).ToNot(HaveOccurred())
				kvJSON, err := json.Marshal(kv)
				Expect(err).ToNot(HaveOccurred())
				modifiedKvJSON, err := patch.Apply(kvJSON)
				Expect(err).ToNot(HaveOccurred())

				// reset the object in preparation for unmarshal,
				// since unmarshal does not guarantee that fields in kv will be removed by the patch
				kv = &v1.KubeVirt{}

				err = json.Unmarshal(modifiedKvJSON, kv)
				Expect(err).ToNot(HaveOccurred())
				return kv, nil
			}).AnyTimes()
	})

	When("with invalid set of flags", func() {
		Context("with empty set of flags", func() {
			It("should fail (return help)", func() {
				cmd := clientcmd.NewRepeatableVirtctlCommand("adm", "log-verbosity")
				err := cmd()
				Expect(err).NotTo(Succeed())
				Expect(err).To(MatchError(ContainSubstring("no flag specified - expecting at least one flag")))
			})
		})

		DescribeTable("should fail handled by cobra", func(args ...string) {
			argStr := strings.Join(args, ",")
			cmd := clientcmd.NewRepeatableVirtctlCommand("adm", "log-verbosity", argStr)
			err := cmd()
			Expect(err).NotTo(Succeed())
		},
			Entry("reset and all coexist", "--reset", "--all=3"),
			Entry("invalid argument (negative verbosity)", "--virt-api=-1"),
			Entry("invalid argument (character)", "--virt-api=a"),
			Entry("unknown flag", "--node"),
			Entry("invalid flag format", "--all", "3"),
		)

		DescribeTable("should fail handled by error handler", func(output string, args ...string) {
			commandAndArgs := []string{"adm", "log-verbosity"}
			commandAndArgs = append(commandAndArgs, args...)
			_, err := clientcmd.NewRepeatableVirtctlCommandWithOut(commandAndArgs...)()
			Expect(err).NotTo(Succeed())

			Expect(err).To(MatchError(ContainSubstring(output)))
		},
			Entry("show and set mix", "show and set cannot coexist", "--virt-handler", "--virt-launcher=3"),
			Entry("show and reset mix", "show and reset cannot coexist", "--reset", "--virt-launcher"),
			Entry("10 or above verbosity", "virt-api: log verbosity must be 0-9", "--virt-api=10"),
		)
	})

	When("no logVerbosity field in the KubeVirt CR", func() {
		DescribeTable("show operation", func(output []int, args ...string) {
			// should show the logVerbosity for components from the KubeVirt CR
			// should show the unattended verbosity, so fill them with default verbosity (2)
			commandAndArgs := []string{"adm", "log-verbosity"}
			commandAndArgs = append(commandAndArgs, args...)
			bytes, err := clientcmd.NewRepeatableVirtctlCommandWithOut(commandAndArgs...)()
			Expect(err).To(Succeed())

			message := createOutputMessage(output) // create an expected output message
			Expect(string(bytes)).To(ContainSubstring(*message))
		},
			Entry("all components", []int{2, 2, 2, 2, 2}, "--all"),
			Entry("one component (1st component (i.e. virt-api))", []int{2, -1, -1, -1, -1}, "--virt-api"),
			Entry("one component (last component (i.e. virt-operator))", []int{-1, -1, -1, -1, 2}, "--virt-operator"),
			Entry("two components", []int{-1, 2, 2, -1, -1}, "--virt-controller", "--virt-handler"),
			Entry("all + one component", []int{2, 2, 2, 2, 2}, "--all", "--virt-launcher"),
		)

		Describe("set operation", func() {
			Context("reset", func() {
				It("do nothing", func() {
					// patch = {}
					// do nothing (do not call Patch method)
					cmd := clientcmd.NewRepeatableVirtctlCommand("adm", "log-verbosity", "--reset")
					Expect(cmd()).To(Succeed())
					Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity).To(BeNil())
				})
			})
			DescribeTable("set", func(output []uint, args ...string) {
				// should add logVerbosity field for the specified components in the KubeVirt CR
				commandAndArgs := []string{"adm", "log-verbosity"}
				commandAndArgs = append(commandAndArgs, args...)
				cmd := clientcmd.NewRepeatableVirtctlCommand(commandAndArgs...)
				Expect(cmd()).To(Succeed())

				expectAllComponentVerbosity(kv, output) // check the verbosity of all components if it is expected
			},
				Entry("one component (1st component (i.e. virt-api))", []uint{1, 0, 0, 0, 0}, "--virt-api=1"),
				Entry("one component (last component (i.e. virt-operator))", []uint{0, 0, 0, 0, 2}, "--virt-operator=2"),
				Entry("two components", []uint{0, 3, 4, 0, 0}, "--virt-controller=3", "--virt-handler=4"),
				Entry("two components", []uint{0, 0, 0, 5, 6}, "--virt-launcher=5", "--virt-operator=6"),
				Entry("all components", []uint{7, 7, 7, 7, 7}, "--all=7"),
			)
		})
	})

	When("existing logVerbosity in the KubeVirt CR", func() {
		BeforeEach(func() {
			lv := &v1.LogVerbosity{
				VirtAPI:        5,
				VirtController: 6,
				VirtLauncher:   3,
				VirtOperator:   4,
			}
			kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity = lv
		})

		DescribeTable("show operation", func(output []int, args ...string) {
			// should show the logVerbosity for components from the KubeVirt CR
			// get and show the attended verbosity
			// show the default verbosity (2), when the logVerbosity is unattended
			commandAndArgs := []string{"adm", "log-verbosity"}
			commandAndArgs = append(commandAndArgs, args...)
			bytes, err := clientcmd.NewRepeatableVirtctlCommandWithOut(commandAndArgs...)()
			Expect(err).To(Succeed())

			message := createOutputMessage(output)
			Expect(string(bytes)).To(ContainSubstring(*message))
		},
			Entry("all components", []int{5, 6, 2, 3, 4}, "--all"),
			Entry("one component attended verbosity", []int{5, -1, -1, -1, -1}, "--virt-api"),
			Entry("one component unattended verbosity", []int{-1, -1, 2, -1, -1}, "--virt-handler"),
			Entry("two components with one unattended verbosity", []int{-1, 6, 2, -1, -1}, "--virt-handler", "--virt-controller"),
			// corner case
			Entry("all components with default argument (equals show operation)", []int{5, 6, 2, 3, 4}, "--all=18446744073709551615"),
		)

		Describe("set operation", func() {
			DescribeTable("set operation", func(output []uint, args ...string) {
				// should add logVerbosity filed for the specified components in the KubeVirt CR
				commandAndArgs := []string{"adm", "log-verbosity"}
				commandAndArgs = append(commandAndArgs, args...)
				cmd := clientcmd.NewRepeatableVirtctlCommand(commandAndArgs...)
				Expect(cmd()).To(Succeed())

				expectAllComponentVerbosity(kv, output)
			},
				Entry("reset", []uint{0, 0, 0, 0, 0}, "--reset"), // CR's logVerbosity field is replaced by {}. logVerbosity struct of each filed is 0.
				Entry("one component (1st component (i.e. virt-api))", []uint{1, 6, 0, 3, 4}, "--virt-api=1"),
				Entry("one component (last component (i.e. virt-operator))", []uint{5, 6, 0, 3, 2}, "--virt-operator=2"),
				Entry("one component (filled in unattended verbosity)", []uint{5, 6, 8, 3, 4}, "--virt-handler=8"),
				Entry("all components", []uint{7, 7, 7, 7, 7}, "--all=7"),
				Entry("two components", []uint{5, 0, 9, 3, 4}, "--virt-controller=0", "--virt-handler=9"),
				Entry("set all and then set two components", []uint{9, 0, 8, 8, 8}, "--all=8", "--virt-api=9", "--virt-controller=0"),
				Entry("reset and then set two components", []uint{0, 0, 1, 2, 0}, "--reset", "--virt-handler=1", "--virt-launcher=2"),
				// corner case
				Entry("two same operations (come down to one operation)", []uint{3, 6, 0, 3, 4}, "--virt-api=3", "--virt-api=3"),
			)
		})
	})
})

func expectAllComponentVerbosity(kv *v1.KubeVirt, output []uint) {
	Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity.VirtAPI).To(Equal(output[0]))
	Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity.VirtController).To(Equal(output[1]))
	Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity.VirtHandler).To(Equal(output[2]))
	Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity.VirtLauncher).To(Equal(output[3]))
	Expect(kv.Spec.Configuration.DeveloperConfiguration.LogVerbosity.VirtOperator).To(Equal(output[4]))
}

// create an expected output message
func createOutputMessage(output []int) *string {
	// mapping table from virtComponent to name of virt component
	var componentToName = [virtComponentNum - 1]string{
		"virt-api",
		"virt-controller",
		"virt-handler",
		"virt-launcher",
		"virt-operator",
	}

	var message string
	var component virtComponent
	for component = virtAPI; component < all; component++ {
		if output[int(component)] == -1 {
			continue
		}
		// output format is like:
		// virt-api=1
		// virt-controller=2
		message += fmt.Sprintf("%s=%d\n", componentToName[component], uint(output[int(component)]))
	}
	return &message
}

func NewKubeVirtWithoutVerbosity(namespace, name string) *v1.KubeVirt {
	return &v1.KubeVirt{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       "KubeVirt",
			APIVersion: v1.GroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1.KubeVirtSpec{
			ImageTag: "devel",
			Configuration: v1.KubeVirtConfiguration{
				DeveloperConfiguration: &v1.DeveloperConfiguration{},
			},
		},
	}
}
