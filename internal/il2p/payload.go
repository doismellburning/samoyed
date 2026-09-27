// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

/*--------------------------------------------------------------------------------
 *
 * Purpose:	Functions dealing with the payload.
 *
 *--------------------------------------------------------------------------------*/

type PayloadProperties struct {
	PayloadByteCount      int // Total size, 0 thru 1023
	payload_block_count   int
	SmallBlockSize        int
	LargeBlockSize        int
	LargeBlockCount       int
	SmallBlockCount       int
	ParitySymbolsPerBlock int // 2, 4, 6, 8, 16
}

/*--------------------------------------------------------------------------------
 *
 * Function:	PayloadCompute
 *
 * Purpose:	Compute number and sizes of data blocks based on total size.
 *
 * Inputs:	payload_size	0 to 1023.  (MaxPayloadSize)
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * Outputs:	*p		Payload block sizes and counts.
 *				Number of parity symbols per block.
 *
 * Returns:	Number of bytes in the encoded format.
 *		Could be 0 for no payload blocks.
 *		-1 for error (i.e. invalid unencoded size: <0 or >1023)
 *
 *--------------------------------------------------------------------------------*/

func PayloadCompute(payload_size int, max_fec int) (*PayloadProperties, int) {
	var p = new(PayloadProperties)

	if payload_size < 0 || payload_size > maxPayloadSize {
		return p, -1
	}

	if payload_size == 0 {
		return p, 0
	}

	if max_fec != 0 {
		p.PayloadByteCount = payload_size
		p.payload_block_count = (p.PayloadByteCount + 238) / 239
		p.SmallBlockSize = p.PayloadByteCount / p.payload_block_count
		p.LargeBlockSize = p.SmallBlockSize + 1
		p.LargeBlockCount = p.PayloadByteCount - (p.payload_block_count * p.SmallBlockSize)
		p.SmallBlockCount = p.payload_block_count - p.LargeBlockCount
		p.ParitySymbolsPerBlock = 16
	} else {
		p.PayloadByteCount = payload_size
		p.payload_block_count = (p.PayloadByteCount + 246) / 247
		p.SmallBlockSize = p.PayloadByteCount / p.payload_block_count
		p.LargeBlockSize = p.SmallBlockSize + 1
		p.LargeBlockCount = p.PayloadByteCount - (p.payload_block_count * p.SmallBlockSize)
		p.SmallBlockCount = p.payload_block_count - p.LargeBlockCount
		//p.parity_symbols_per_block = (p.small_block_size / 32) + 2;  // Looks like error in documentation

		// It would work if the number of parity symbols was based on large block size.

		if p.SmallBlockSize <= 61 {
			p.ParitySymbolsPerBlock = 2
		} else if p.SmallBlockSize <= 123 {
			p.ParitySymbolsPerBlock = 4
		} else if p.SmallBlockSize <= 185 {
			p.ParitySymbolsPerBlock = 6
		} else if p.SmallBlockSize <= 247 {
			p.ParitySymbolsPerBlock = 8
		} else {
			// Should not happen.  But just in case...
			logrus.WithField("small_block_size", p.SmallBlockSize).Error("IL2P parity symbol per payload block error")

			return p, -1
		}
	}

	// Return the total size for the encoded format.

	return p, (p.SmallBlockCount*(p.SmallBlockSize+p.ParitySymbolsPerBlock) +
		p.LargeBlockCount*(p.LargeBlockSize+p.ParitySymbolsPerBlock))
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_encode_payload
 *
 * Purpose:	Split payload into multiple blocks such that each set
 *		of data and parity symbols fit into a 255 byte RS block.
 *
 * Inputs:	payload	Slice of bytes.
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * Returns:	Encoded payload for transmission.
 *				Up to IL2P_MAX_ENCODED_SIZE bytes.
 *
 * Returns:	-1 for error (i.e. invalid size)
 *		0 for no blocks.  (i.e. size zero)
 *		Number of bytes generated.  Maximum IL2P_MAX_ENCODED_SIZE.
 *
 * Note:	I interpreted the protocol spec as saying the LFSR state is retained
 *		between data blocks.  During interoperability testing, I found that
 *		was not the case.  It is reset for each data block.
 *
 *--------------------------------------------------------------------------------*/

func il2p_encode_payload(payload []byte, max_fec int) ([]byte, int) {
	var payload_size = len(payload)

	if payload_size > maxPayloadSize {
		return nil, -1
	}

	if payload_size == 0 {
		return nil, 0
	}

	// Determine number of blocks and sizes.

	var ipp, e = PayloadCompute(payload_size, max_fec)
	if e <= 0 {
		return nil, e
	}

	var pin = payload
	var pout []byte
	var encoded_length = 0

	// First the large blocks.

	for range ipp.LargeBlockCount {
		var scram = il2p_scramble_block(pin[:ipp.LargeBlockSize])
		pout = append(pout, scram...)

		pin = pin[ipp.LargeBlockSize:]

		encoded_length += ipp.LargeBlockSize

		var parity, err = il2p_encode_rs(scram, ipp.ParitySymbolsPerBlock)
		if err != nil {
			logrus.WithError(err).Error("Cannot encode an IL2P payload block")

			return nil, -1
		}

		pout = append(pout, parity...)

		encoded_length += ipp.ParitySymbolsPerBlock
	}

	// Then the small blocks.

	for range ipp.SmallBlockCount {
		var scram = il2p_scramble_block(pin[:ipp.SmallBlockSize])
		pout = append(pout, scram...)

		pin = pin[ipp.SmallBlockSize:]
		encoded_length += ipp.SmallBlockSize

		var parity, err = il2p_encode_rs(scram, ipp.ParitySymbolsPerBlock)
		if err != nil {
			logrus.WithError(err).Error("Cannot encode an IL2P payload block")

			return nil, -1
		}

		pout = append(pout, parity...)

		encoded_length += ipp.ParitySymbolsPerBlock
	}

	return pout, encoded_length
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_decode_payload
 *
 * Purpose:	Extract original data from encoded payload.
 *
 * Inputs:	received	Array of bytes.  Size is unknown but in practice it
 *				must not exceed IL2P_MAX_ENCODED_SIZE.
 *		payload_size	0 to 1023.  (MaxPayloadSize)
 *				Expected result size based on header.
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * In/Out:	symbols_corrected	Number of symbols corrected.
 *
 *
 * Returns:	payload_out	Recovered payload.
 *
 * Returns:	Number of bytes extracted.  Should be same as payload_size going in.
 *		-3 for unexpected internal inconsistency.
 *		-2 for unable to recover from signal corruption.
 *		-1 for invalid size.
 *		0 for no blocks.  (i.e. size zero)
 *
 * Description:	Each block is scrambled separately but the LFSR state is carried
 *		from the first payload block to the next.
 *
 *--------------------------------------------------------------------------------*/

func il2p_decode_payload(received []byte, payload_size int, max_fec int, symbols_corrected *int) ([]byte, int) {
	// Determine number of blocks and sizes.
	var ipp, e = PayloadCompute(payload_size, max_fec)
	if e <= 0 {
		return nil, e
	}

	var pin = received
	var pout []byte
	var decoded_length = 0
	var failed = false

	// First the large blocks.

	for range ipp.LargeBlockCount {
		var corrected_block, e = il2p_decode_rs(pin[:ipp.LargeBlockSize+ipp.ParitySymbolsPerBlock], ipp.ParitySymbolsPerBlock)

		// dw_printf ("%s:%d: large block decode_rs returned status = %d\n", __FILE__, __LINE__, e);

		if e < 0 {
			failed = true
		}

		*symbols_corrected += e

		var descrambled = il2p_descramble_block(corrected_block)
		pout = append(pout, descrambled...)

		if Debug() >= 2 {
			logrus.WithField("bytes", ipp.LargeBlockSize).Debug("Descrambled large payload block")
			dwutil.HexDump(descrambled)
		}

		pin = pin[ipp.LargeBlockSize+ipp.ParitySymbolsPerBlock:]
		decoded_length += ipp.LargeBlockSize
	}

	// Then the small blocks.

	for range ipp.SmallBlockCount {
		var corrected_block, e = il2p_decode_rs(pin[:ipp.SmallBlockSize+ipp.ParitySymbolsPerBlock], ipp.ParitySymbolsPerBlock)

		// dw_printf ("%s:%d: small block decode_rs returned status = %d\n", __FILE__, __LINE__, e);

		if e < 0 {
			failed = true
		}

		*symbols_corrected += e

		var descrambled = il2p_descramble_block(corrected_block)
		pout = append(pout, descrambled...)

		if Debug() >= 2 {
			logrus.WithField("bytes", ipp.SmallBlockSize).Debug("Descrambled small payload block")
			dwutil.HexDump(descrambled)
		}

		pin = pin[ipp.SmallBlockSize+ipp.ParitySymbolsPerBlock:]
		decoded_length += ipp.SmallBlockSize
	}

	if failed {
		//dw_printf ("%s:%d: failed = %0x\n", __FILE__, __LINE__, failed);
		return nil, -2
	}

	if decoded_length != payload_size {
		logrus.WithFields(logrus.Fields{
			"decoded_length": decoded_length,
			"payload_size":   payload_size,
		}).Error("IL2P internal error: Decoded payload is the wrong length")

		return nil, -3
	}

	return pout, decoded_length
}
