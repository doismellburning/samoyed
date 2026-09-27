package direwolf

// Config from direwolf.h - probably belongs elsewhere

/*
 * Maximum number of audio devices.
 * Three is probably adequate for standard version.
 * Larger reasonable numbers should also be fine.
 *
 * For example, if you wanted to use 4 audio devices at once, change this to 4.
 */

const MAX_ADEVS = 3

/*
 * Maximum number of radio channels.
 * Note that there could be gaps.
 * Suppose audio device 0 was in mono mode and audio device 1 was stereo.
 * The channels available would be:
 *
 *	ADevice 0:	channel 0
 *	ADevice 1:	left = 2, right = 3
 */

const MAX_RADIO_CHANS = ((MAX_ADEVS) * 2)

const MAX_TOTAL_CHANS = 16 // v1.7 allows additional virtual channels which are connected
// to something other than radio modems.
// Total maximum channels is based on the 4 bit KISS field.
// Someone with very unusual requirements could increase this and
// use only the AGW network protocol.

/*
 * Maximum number of rigs.
 */

const MAX_RIGS = MAX_RADIO_CHANS

/*
 * Maximum number of modems per channel.
 * I called them "subchannels" (in the code) because
 * it is short and unambiguous.
 * Nothing magic about the number.  Could be larger
 * but CPU demands might be overwhelming.
 */

const MAX_SUBCHANS = 9

/*
 * Each one of these can have multiple slicers, at
 * different levels, to compensate for different
 * amplitudes of the AFSK tones.
 * Initially used same number as subchannels but
 * we could probably trim this down a little
 * without impacting performance.
 */

const MAX_SLICERS = 9
