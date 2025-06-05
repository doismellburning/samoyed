

/* telemetry.h */

void telemetry_data_original (char *station, char *info, int quiet, char *output, size_t outputsize, char *comment, size_t commentsize);
 
void telemetry_data_base91 (char *station, char *cdata, char *output, size_t outputsize);
 
void telemetry_name_message (char *station, char *msg);
 
void telemetry_unit_label_message (char *station, char *msg);

void telemetry_coefficents_message (char *station, char *msg, int quiet);

void telemetry_bit_sense_message (char *station, char *msg, int quiet);


/*
 * Metadata for telemetry data.
 */

#define T_NUM_ANALOG 5				/* Number of analog channels. */
#define T_NUM_DIGITAL 8				/* Number of digital channels. */

#define T_STR_LEN 32				/* Max len for labels and units. */

struct t_metadata_s {
	int magic1;

	struct t_metadata_s * pnext;		/* Next in linked list. */

	char station[AX25_MAX_ADDR_LEN];	/* Station name with optional SSID. */

	char project[40];			/* Description for data. */
						/* "Project Name" or "project title" in the spec. */

	char name[T_NUM_ANALOG+T_NUM_DIGITAL][T_STR_LEN];
						/* Names for channels.  e.g. Battery, Temperature */

	char unit[T_NUM_ANALOG+T_NUM_DIGITAL][T_STR_LEN];
						/* Units for channels.  e.g. Volts, Deg.C */

	float coeff[T_NUM_ANALOG][3];		/* a, b, c coefficients for scaling. */

	int coeff_ndp[T_NUM_ANALOG][3];		/* Number of decimal places for above. */

	int sense[T_NUM_DIGITAL];		/* Polarity for digital channels. */

	int magic2;
};
