package l2tp

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	pathIPsecSecrets     = "/etc/ipsec.secrets"
	pathXl2tpdConf       = "/etc/xl2tpd/xl2tpd.conf"
	pathIPsecConf        = "/etc/ipsec.conf"
	pathPppOptionsClient = "/etc/ppp/options.l2tpd.client"
	pathConnectScript    = "/usr/local/bin/l2tp-connect.sh"
	l2tpControlPath      = "/var/run/xl2tpd/l2tp-control"
)

// StepID identifies a single step in the connection flow for progress reporting.
type StepID string

const (
	StepEnsureServices StepID = "ensure_services"
	StepIPsecSecrets   StepID = "ipsec_secrets"
	StepXl2tpdConf     StepID = "xl2tpd_conf"
	StepIPsecConf      StepID = "ipsec_conf"
	StepPppOptions     StepID = "ppp_options"
	StepConnectScript  StepID = "connect_script"
	StepRunScript      StepID = "run_script"
)

// l2tpServices are systemd units that must be running before connect.
var l2tpServices = []string{"strongswan-starter", "xl2tpd"}

// EnsureL2TPServices starts each L2TP service if not already active; requires root.
func EnsureL2TPServices() error {
	for _, svc := range l2tpServices {
		if err := exec.Command("systemctl", "is-active", "--quiet", svc).Run(); err == nil {
			continue
		}
		if startErr := exec.Command("systemctl", "start", svc).Run(); startErr != nil {
			return fmt.Errorf("start %s: %w", svc, startErr)
		}
	}
	return nil
}

// OnStepFunc is called after each connection step completes (stepID, file path or description).
type OnStepFunc func(stepID StepID, path string)

// ApplyConfig writes the five L2TP config files (ipsec, xl2tpd, ppp, connect script) using c; requires root.
func ApplyConfig(c VPNConfig) error {
	return ApplyConfigWithSteps(c, nil)
}

// ApplyConfigWithSteps does ApplyConfig and reports each completed step via onStep; onStep may be nil.
func ApplyConfigWithSteps(c VPNConfig, onStep OnStepFunc) error {
	if c.ServerAddress == "" {
		return fmt.Errorf("server_address is required")
	}
	if err := writeIPsecSecrets(c); err != nil {
		return err
	}
	if onStep != nil {
		onStep(StepIPsecSecrets, pathIPsecSecrets)
	}
	if err := os.MkdirAll(filepath.Dir(pathXl2tpdConf), 0755); err != nil {
		return fmt.Errorf("create xl2tpd dir: %w", err)
	}
	if err := writeXl2tpdConf(c); err != nil {
		return err
	}
	if onStep != nil {
		onStep(StepXl2tpdConf, pathXl2tpdConf)
	}
	if err := writeIPsecConf(c); err != nil {
		return err
	}
	if onStep != nil {
		onStep(StepIPsecConf, pathIPsecConf)
	}
	if err := os.MkdirAll(filepath.Dir(pathPppOptionsClient), 0755); err != nil {
		return fmt.Errorf("create ppp dir: %w", err)
	}
	if err := writePppOptionsClient(c); err != nil {
		return err
	}
	if onStep != nil {
		onStep(StepPppOptions, pathPppOptionsClient)
	}
	if err := os.MkdirAll(filepath.Dir(pathConnectScript), 0755); err != nil {
		return fmt.Errorf("create script dir: %w", err)
	}
	if err := writeConnectScript(c); err != nil {
		return err
	}
	if onStep != nil {
		onStep(StepConnectScript, pathConnectScript)
	}
	return nil
}

func writeIPsecSecrets(c VPNConfig) error {
	usePSK := c.UsePreSharedKey == nil || *c.UsePreSharedKey
	psk := c.PreSharedKey
	if !usePSK {
		psk = ""
	}
	body := `# This file holds shared secrets or RSA private keys for authentication.

# RSA private key for this host, authenticating it to any other host
# which knows the public part.
%any ` + c.ServerAddress + ` : PSK "` + psk + `"
`
	return os.WriteFile(pathIPsecSecrets, []byte(body), 0600)
}

func writeXl2tpdConf(c VPNConfig) error {
	body := `[global]
port = 1701
ipsec saref = yes
debug tunnel = yes
debug packet = yes

[lac active_vpn]
lns = ` + c.ServerAddress + `
pppoptfile = /etc/ppp/options.l2tpd.client
length bit = yes
`
	return os.WriteFile(pathXl2tpdConf, []byte(body), 0644)
}

func writeIPsecConf(c VPNConfig) error {
	body := `config setup

conn active_vpn
    keyexchange=ikev1
    authby=secret
    type=transport
    left=%defaultroute
    leftprotoport=17/1701
    right=` + c.ServerAddress + `
    rightprotoport=17/1701
    auto=add
`
	return os.WriteFile(pathIPsecConf, []byte(body), 0644)
}

func writePppOptionsClient(c VPNConfig) error {
	body := `name ` + c.Username + `
password ` + c.Password + `
ipcp-accept-local
ipcp-accept-remote
refuse-eap
refuse-pap
refuse-chap
require-mschap-v2
# require-mppe-128  # Some MikroTiks prefer this to be optional; try commenting out if it fails
noauth
idle 1800
mtu 1410
mru 1410
defaultroute
usepeerdns
debug
lock
connect-delay 5000
noipv6
`
	return os.WriteFile(pathPppOptionsClient, []byte(body), 0600)
}

func writeConnectScript(c VPNConfig) error {
	body := `#!/bin/bash
set -e
VPN_NAME="active_vpn"
REMOTE_SERVER="` + c.ServerAddress + `"
INTERFACE="ppp0"

echo "--- Starting VPN Connection Sequence ---"

echo "[Step: Start strongswan-starter]"
sudo systemctl start strongswan-starter 2>&1 || { echo "ERROR [strongswan-starter]: start failed (exit $?)"; exit 1; }

echo "[Step: Start xl2tpd]"
sudo systemctl start xl2tpd 2>&1 || { echo "ERROR [xl2tpd]: start failed (exit $?)"; exit 1; }

MY_IP=$(echo $SSH_CONNECTION | awk '{print $1}')
CURRENT_GW=$(ip route show default | awk '/default/ {print $3}')

if [ ! -z "$MY_IP" ]; then
    echo "[Step: SSH lockout route]"
    sudo route add -host $MY_IP gw $CURRENT_GW 2>&1 || echo "Warning: SSH route add failed (exit $?)"
fi

echo "[Step: Initiate L2TP tunnel]"
echo "c $VPN_NAME" | sudo tee /var/run/xl2tpd/l2tp-control 2>&1

echo "[Step: Wait for $INTERFACE]"
for i in {1..15}; do
    if ip addr show $INTERFACE >/dev/null 2>&1; then
        echo "Interface $INTERFACE is UP!"
        break
    fi
    sleep 1
    if [ $i -eq 15 ]; then
        echo "ERROR [ppp interface]: $INTERFACE failed to come up within 15s"
        exit 1
    fi
done

echo "[Step: Add routes]"
sudo route add -host $REMOTE_SERVER gw $CURRENT_GW 2>&1 || { echo "ERROR [route to server]: failed (exit $?)"; exit 1; }
sudo route add default dev $INTERFACE 2>&1 || { echo "ERROR [default route]: failed (exit $?)"; exit 1; }

echo "--- VPN Connected Successfully ---"
echo "[Step: Check IP]"
curl -s --max-time 5 https://ifconfig.me 2>&1 && echo "" || echo "(could not fetch)"
`
	return os.WriteFile(pathConnectScript, []byte(body), 0755)
}

// RunConnectScript executes the connect script and returns combined stdout+stderr and any exec error.
func RunConnectScript() (log string, err error) {
	cmd := exec.Command("/bin/bash", pathConnectScript)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	log = out.String()
	if runErr != nil {
		return log, fmt.Errorf("%w", runErr)
	}
	return log, nil
}

// RunDisconnect sends disconnect for the active_vpn tunnel to xl2tpd; requires root.
func RunDisconnect() error {
	f, err := os.OpenFile(l2tpControlPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte("d active_vpn\n"))
	return err
}
