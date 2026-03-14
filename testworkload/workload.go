//go:build darwin

package testworkload

import (
	"github.com/tmc/apple/x/ane"
	"github.com/tmc/apple/x/ane/mil"
)

// OpenConv compiles a small 1×1 convolution (linear layer) on the ANE.
// The model maps inCh input channels to outCh output channels with spatial=1.
// Caller must call Model.Close when done.
func OpenConv(client *ane.Client, inCh, outCh int) (*ane.Model, error) {
	milText := mil.GenConv(inCh, outCh, 1)
	blob, err := mil.BuildIdentityWeightBlob(inCh)
	if inCh != outCh {
		// For non-square, build a zero-initialized weight blob.
		weights := make([]float32, inCh*outCh)
		// Initialize as identity-like: copy min(inCh,outCh) diagonal.
		n := inCh
		if outCh < n {
			n = outCh
		}
		for i := range n {
			weights[i*inCh+i] = 1.0
		}
		blob, err = mil.BuildWeightBlob(weights, outCh, inCh)
	}
	if err != nil {
		return nil, err
	}
	return client.Compile(ane.CompileOptions{
		ModelType:  ane.ModelTypeMIL,
		MILText:    []byte(milText),
		WeightBlob: blob,
	})
}

// OpenIdentity compiles a small identity model on the ANE.
// The model passes channels float values through unchanged.
// Caller must call Model.Close when done.
func OpenIdentity(client *ane.Client, channels int) (*ane.Model, error) {
	milText := mil.GenIdentity(channels, 1)
	blob, err := mil.BuildIdentityWeightBlob(channels)
	if err != nil {
		return nil, err
	}
	return client.Compile(ane.CompileOptions{
		ModelType:  ane.ModelTypeMIL,
		MILText:    []byte(milText),
		WeightBlob: blob,
	})
}
