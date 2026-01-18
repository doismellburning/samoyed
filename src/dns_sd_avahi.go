package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Announce the KISS over TCP service using DNS-SD via Avahi
 *
 * Description:
 *
 *     Most people have typed in enough IP addresses and ports by now, and
 *     would rather just select an available TNC that is automatically
 *     discovered on the local network.  Even more so on a mobile device
 *     such an Android or iOS phone or tablet.
 *
 *     On Linux, the announcement can be made through Avahi, the mDNS
 *     framework commonly deployed on Linux systems.
 *
 *     This is largely based on the publishing example of the Avahi library.
 */

// #include <stdlib.h>
// #include <stdio.h>
// #include <pthread.h>
// #include <avahi-client/client.h>
// #include <avahi-client/publish.h>
// #include <avahi-common/simple-watch.h>
// #include <avahi-common/alternative.h>
// #include <avahi-common/malloc.h>
// #include <avahi-common/error.h>
// void entry_group_callback(AvahiEntryGroup *g, AvahiEntryGroupState state, AVAHI_GCC_UNUSED void *userdata);
// void client_callback(AvahiClient *c, AvahiClientState state, AVAHI_GCC_UNUSED void * userdata);
import "C"

import (
	"unsafe"
)

var avahiEntryGroup *C.AvahiEntryGroup
var avahiSimplePoll *C.AvahiSimplePoll
var avahiClient *C.AvahiClient
var avahiName *C.char
var avahiKISSPort C.uint16_t

const AVAHI_PRINT_PREFIX = "DNS-SD: Avahi: "
const DNS_SD_SERVICE = "_kiss-tnc._tcp"

//export entry_group_callback
func entry_group_callback(g *C.AvahiEntryGroup, state C.AvahiEntryGroupState, userdata unsafe.Pointer) {
	Assert(g == avahiEntryGroup || avahiEntryGroup == nil)
	avahiEntryGroup = g

	/* Called whenever the entry group state changes */
	switch state {
	case C.AVAHI_ENTRY_GROUP_ESTABLISHED:
		/* The entry group has been established successfully */
		text_color_set(DW_COLOR_INFO)
		dw_printf(AVAHI_PRINT_PREFIX+"Service '%s' successfully registered.\n", C.GoString(avahiName))
	case C.AVAHI_ENTRY_GROUP_COLLISION:
		{
			/* A service name collision with a remote service
			 * happened. Let's pick a new name. */
			var n = C.avahi_alternative_service_name(avahiName)
			C.avahi_free(unsafe.Pointer(avahiName))
			avahiName = n
			text_color_set(DW_COLOR_INFO)
			dw_printf(AVAHI_PRINT_PREFIX+"Service name collision, renaming service to '%s'\n", C.GoString(avahiName))
			/* And recreate the services */
			create_services(C.avahi_entry_group_get_client(g))
			break
		}
	case C.AVAHI_ENTRY_GROUP_FAILURE:
		text_color_set(DW_COLOR_ERROR)
		dw_printf(AVAHI_PRINT_PREFIX+"Entry group failure: %s\n", C.GoString(C.avahi_strerror(C.avahi_client_errno(C.avahi_entry_group_get_client(g)))))
		/* Some kind of failure happened while we were registering our services */
		C.avahi_simple_poll_quit(avahiSimplePoll)
	case C.AVAHI_ENTRY_GROUP_UNCOMMITED: //nolint: misspell
	case C.AVAHI_ENTRY_GROUP_REGISTERING:

	}
}

func create_services(c *C.AvahiClient) {
	Assert(c != nil)

	/* If this is the first time we're called, let's create a new
	 * entry group if necessary */
	if avahiEntryGroup == nil {
		var callback = C.AvahiEntryGroupCallback(C.entry_group_callback)
		if avahiEntryGroup = C.avahi_entry_group_new(c, callback, nil); avahiEntryGroup == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf(AVAHI_PRINT_PREFIX+"avahi_entry_group_new() failed: %s\n", C.GoString(C.avahi_strerror(C.avahi_client_errno(c))))
			C.avahi_simple_poll_quit(avahiSimplePoll)
			return
		}
	} else {
		C.avahi_entry_group_reset(avahiEntryGroup)
	}

	/* If the group is empty (either because it was just created, or
	 * because it was reset previously, add our entries.  */
	if C.avahi_entry_group_is_empty(avahiEntryGroup) != 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf(AVAHI_PRINT_PREFIX+"Announcing KISS TCP on port %d as '%s'\n", avahiKISSPort, C.GoString(avahiName))

		/* Announce with AVAHI_PROTO_INET instead of AVAHI_PROTO_UNSPEC, since Dire Wolf currently
		 * only listens on IPv4.
		 */

		if ret := C.avahi_entry_group_add_service_strlst(avahiEntryGroup, C.AVAHI_IF_UNSPEC, C.AVAHI_PROTO_INET, 0, avahiName, C.CString(DNS_SD_SERVICE), nil, nil, avahiKISSPort, nil); ret < 0 {
			if ret == C.AVAHI_ERR_COLLISION {
				/* A service name collision with a local service happened. Let's
				 * pick a new name */
				var n = C.avahi_alternative_service_name(avahiName)
				C.avahi_free(unsafe.Pointer(avahiName))
				avahiName = n
				text_color_set(DW_COLOR_INFO)
				dw_printf(AVAHI_PRINT_PREFIX+"Service name collision, renaming service to '%s'\n", C.GoString(avahiName))
				C.avahi_entry_group_reset(avahiEntryGroup)
				create_services(c)
				return
			}
			text_color_set(DW_COLOR_ERROR)
			dw_printf(AVAHI_PRINT_PREFIX+"Failed to add _kiss-tnc._tcp service: %s\n", C.GoString(C.avahi_strerror(ret)))
			C.avahi_simple_poll_quit(avahiSimplePoll)
			return
		}

		/* Tell the server to register the service */
		if ret := C.avahi_entry_group_commit(avahiEntryGroup); ret < 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf(AVAHI_PRINT_PREFIX+"Failed to commit entry group: %s\n", C.GoString(C.avahi_strerror(ret)))
			C.avahi_simple_poll_quit(avahiSimplePoll)
			return
		}
	}
}

//export client_callback
func client_callback(c *C.AvahiClient, state C.AvahiClientState, userdata unsafe.Pointer) {
	Assert(c != nil)
	/* Called whenever the client or server state changes */
	switch state {
	case C.AVAHI_CLIENT_S_RUNNING:
		/* The server has startup successfully and registered its host
		 * name on the network, so it's time to create our services */
		create_services(c)
	case C.AVAHI_CLIENT_FAILURE:
		text_color_set(DW_COLOR_ERROR)
		dw_printf(AVAHI_PRINT_PREFIX+"Client failure: %s\n", C.GoString(C.avahi_strerror(C.avahi_client_errno(c))))
		C.avahi_simple_poll_quit(avahiSimplePoll)
	case C.AVAHI_CLIENT_S_COLLISION:
		/* Let's drop our registered services. When the server is back
		 * in AVAHI_SERVER_RUNNING state we will register them
		 * again with the new host name. */
	case C.AVAHI_CLIENT_S_REGISTERING:
		/* The server records are now being established. This
		 * might be caused by a host name change. We need to wait
		 * for our own records to register until the host name is
		 * properly established. */
		if avahiEntryGroup != nil {
			C.avahi_entry_group_reset(avahiEntryGroup)
		}
	case C.AVAHI_CLIENT_CONNECTING:

	}
}

func avahiCleanup() {
	/* Cleanup things */
	if avahiClient != nil {
		C.avahi_client_free(avahiClient)
	}

	if avahiSimplePoll != nil {
		C.avahi_simple_poll_free(avahiSimplePoll)
	}

	C.avahi_free(unsafe.Pointer(avahiName))
}

func avahi_mainloop() {
	/* Run the main loop */
	C.avahi_simple_poll_loop(avahiSimplePoll)

	avahiCleanup()
}

func dns_sd_announce(mc *misc_config_s) {
	text_color_set(DW_COLOR_DEBUG)
	avahiKISSPort = C.uint16_t(mc.kiss_port[0]) // FIXME:  Quick hack until I can handle multiple TCP ports properly.

	/* Allocate main loop object */
	avahiSimplePoll = C.avahi_simple_poll_new()
	if avahiSimplePoll == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf(AVAHI_PRINT_PREFIX + "Failed to create Avahi simple poll object.\n")
		avahiCleanup()
		return
	}

	if mc.dns_sd_name[0] != 0 {
		avahiName = C.avahi_strdup(&mc.dns_sd_name[0])
	} else {
		avahiName = C.CString(dns_sd_default_service_name())
	}

	/* Allocate a new client */
	var err C.int
	avahiClient = C.avahi_client_new(C.avahi_simple_poll_get(avahiSimplePoll), 0, C.AvahiClientCallback(C.client_callback), nil, &err)

	/* Check whether creating the client object succeeded */
	if avahiClient == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf(AVAHI_PRINT_PREFIX+"Failed to create Avahi client: %s\n", C.GoString(C.avahi_strerror(err)))
		avahiCleanup()
		return
	}

	go avahi_mainloop()
}
