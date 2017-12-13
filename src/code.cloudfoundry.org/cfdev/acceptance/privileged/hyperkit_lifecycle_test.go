package privileged_test

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	. "code.cloudfoundry.org/cfdev/acceptance"
)

var _ = Describe("hyperkit lifecycle", func() {
	var (
		cfdevHome       string
		linuxkitPidPath string
		stateDir        string
		cacheDir        string
	)

	BeforeEach(func() {
		me, err := user.Current()
		Expect(err).ToNot(HaveOccurred())
		Expect(me.Uid).To(Equal("0"), "lifecycle tests should be run as root")

		RemoveIPAliases(BoshDirectorIP, CFRouterIP)

		cfdevHome = CreateTempCFDevHomeDir()
		cacheDir = filepath.Join(cfdevHome, "cache")
		stateDir = filepath.Join(cfdevHome, "state")
		linuxkitPidPath = filepath.Join(stateDir, "linuxkit.pid")

		SetupDependencies(cacheDir)
	})

	AfterEach(func() {
		gexec.KillAndWait()
		pid := PidFromFile("linuxkit.pid")

		if pid != 0 {
			syscall.Kill(int(-pid), syscall.SIGKILL)
		}

		os.RemoveAll(cfdevHome)

		RemoveIPAliases(BoshDirectorIP, CFRouterIP)
	})

	It("starts and stops the vm", func() {
		command := exec.Command(cliPath, "start")
		command.Env = append(os.Environ(),
			fmt.Sprintf("CFDEV_SKIP_ASSET_CHECK=true"),
			fmt.Sprintf("CFDEV_HOME=%s", cfdevHome))

		session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)

		Expect(err).ShouldNot(HaveOccurred())
		Eventually(linuxkitPidPath, 10, 1).Should(BeAnExistingFile())

		// FYI - this will take time until we use thin provisioned disks
		hyperkitPidPath := filepath.Join(stateDir, "hyperkit.pid")
		Eventually(hyperkitPidPath, 120, 1).Should(BeAnExistingFile())

		By("waiting for garden to listen")
		EventuallyShouldListenAt("http://"+GardenIP+":7777", 30)

		By("waiting for bosh to listen")
		EventuallyShouldListenAt("https://"+BoshDirectorIP+":25555", 240)

		By("waiting for cf router to listen")
		EventuallyShouldListenAt("http://"+CFRouterIP+":80", 1200)

		By("waiting for cfdev cli to exit when the deploy finished")
		Eventually(session, 300).Should(gexec.Exit(0))

		linuxkitPid := PidFromFile(linuxkitPidPath)
		hyperkitPid := PidFromFile(hyperkitPidPath)

		By("deploy finished - stopping...")
		command = exec.Command(cliPath, "stop")
		command.Env = append(os.Environ(),
			fmt.Sprintf("CFDEV_HOME=%s", cfdevHome))

		session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
		Expect(err).ShouldNot(HaveOccurred())
		Eventually(session).Should(gexec.Exit(0))

		//ensure pid is not running
		EventuallyProcessStops(linuxkitPid)
		EventuallyProcessStops(hyperkitPid)
	})
})

func RemoveIPAliases(aliases ...string) {
	for _, alias := range aliases {
		cmd := exec.Command("ifconfig", "lo0", "inet", alias+"/32", "remove")
		session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())
		Eventually(session).Should(gexec.Exit())
	}
}