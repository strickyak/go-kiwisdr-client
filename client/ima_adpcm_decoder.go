package client

type ImaAdpcmDecoder struct {
	index int
	prev  int
}

var stepSizeTable = []int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34,
	37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143,
	157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494,
	544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552,
	1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026,
	4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
	11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623,
	27086, 29794, 32767,
}

var indexAdjustTable = []int{
	-1, -1, -1, -1, //# +0 - +3, decrease the step size
	2, 4, 6, 8, //# +4 - +7, increase the step size
	-1, -1, -1, -1, //# -0 - -3, decrease the step size
	2, 4, 6, 8, //# -4 - -7, increase the step size
}

func clamp(x, xmin, xmax int) int {
	if x < xmin {
		return xmin
	}
	if x > xmax {
		return xmax
	}
	return x
}

func (dec *ImaAdpcmDecoder) Decode(data []byte, skip int) []int16 {
	data_length := len(data) - skip
	// final short[] samples = new short[data_length*2];
	samples := make([]int16, data_length*2)
	for i := 0; i < data_length; i++ {
		samples[2*i+0] = dec.decodeSample(data[i+skip] & 0x0F)
		samples[2*i+1] = dec.decodeSample((data[i+skip] >> 4) & 0x0F)
	}
	return samples
}

func (dec *ImaAdpcmDecoder) decodeSample(code byte) int16 {
	step := stepSizeTable[dec.index]
	dec.index = clamp(dec.index+indexAdjustTable[code], 0, len(stepSizeTable)-1)
	difference := step >> 3
	if (code & 1) != 0 {
		difference += step >> 2
	}
	if (code & 2) != 0 {
		difference += step >> 1
	}
	if (code & 4) != 0 {
		difference += step
	}
	if (code & 8) != 0 {
		difference = -difference
	}
	sample := int16(clamp(dec.prev+difference, -32768, 32767))
	dec.prev = int(sample)
	return sample
}
