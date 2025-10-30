

/* demod_9600.h */


#include "fsk_demod_state.h"


void demod_9600_init (enum modem_t modem_type, int original_sample_rate, int upsample, int baud, struct demodulator_state_s *D);

void demod_9600_process_sample (int chan, int sam, int upsample, struct demodulator_state_s *D);




