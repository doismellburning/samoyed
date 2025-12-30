package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Announce the KISS over TCP service using DNS-SD, common functions
 *
 * Description:
 *
 *     Most people have typed in enough IP addresses and ports by now, and
 *     would rather just select an available TNC that is automatically
 *     discovered on the local network.  Even more so on a mobile device
 *     such an Android or iOS phone or tablet.
 *
 *     This module contains common functions needed on Linux and MacOS.
 */

import (
	"os"
	"strings"
)

/* Get a default service name to publish. By default,
 * "Dire Wolf on <hostname>", or just "Dire Wolf" if hostname cannot
 * be obtained.
 */
func dns_sd_default_service_name() string {
	var hostname, hostnameErr = os.Hostname()
	if hostnameErr != nil {
		return "Dire Wolf"
	}

	// on some systems, an FQDN is returned; remove domain part
	hostname, _, _ = strings.Cut(hostname, ".")

	return "Samoyed on " + hostname
}
