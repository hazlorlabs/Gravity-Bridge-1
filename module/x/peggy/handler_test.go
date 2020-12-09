package peggy

import (
	"math"
	"testing"
	"time"

	"github.com/althea-net/peggy/module/x/peggy/keeper"
	"github.com/althea-net/peggy/module/x/peggy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleValsetRequest(t *testing.T) {
	var (
		myOrchestratorAddr sdk.AccAddress = make([]byte, sdk.AddrLen)
		myCosmosAddr, _                   = sdk.AccAddressFromBech32("cosmos1990z7dqsvh8gthw9pa5sn4wuy2xrsd80mg5z6y")
		myValAddr                         = sdk.ValAddress(myOrchestratorAddr) // revisit when proper mapping is impl in keeper
		myBlockTime                       = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
		myBlockHeight      int64          = 200
	)

	k, ctx, _ := keeper.CreateTestEnv(t)
	k.StakingKeeper = keeper.NewStakingKeeperMock(myValAddr)
	h := NewHandler(k)
	msg := &types.MsgValsetRequest{Requester: myCosmosAddr.String()}
	ctx = ctx.WithBlockTime(myBlockTime).WithBlockHeight(myBlockHeight)
	res, err := h(ctx, msg)
	// then
	require.NoError(t, err)
	nonce := types.UInt64FromBytes(res.Data)
	require.False(t, nonce == 0)
	require.Equal(t, uint64(myBlockHeight), nonce)
	// and persisted
	valset := k.GetValsetRequest(ctx, nonce)
	require.NotNil(t, valset)
	assert.Equal(t, nonce, valset.Nonce)
	require.Len(t, valset.Members, 1)
	assert.Equal(t, []uint64{math.MaxUint32}, types.BridgeValidators(valset.Members).GetPowers())
	assert.Equal(t, "", valset.Members[0].EthereumAddress)
}

func TestHandleCreateEthereumClaimsSingleValidator(t *testing.T) {
	var (
		myOrchestratorAddr sdk.AccAddress = make([]byte, sdk.AddrLen)
		myCosmosAddr, _                   = sdk.AccAddressFromBech32("cosmos16ahjkfqxpp6lvfy9fpfnfjg39xr96qett0alj5")
		myValAddr                         = sdk.ValAddress(myOrchestratorAddr) // revisit when proper mapping is impl in keeper
		myNonce                           = uint64(1)
		anyETHAddr                        = "0xf9613b532673Cc223aBa451dFA8539B87e1F666D"
		tokenETHAddr                      = "0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e"
		myBlockTime                       = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
	)
	k, ctx, keepers := keeper.CreateTestEnv(t)
	k.StakingKeeper = keeper.NewStakingKeeperMock(myValAddr)
	h := NewHandler(k)

	myErc20 := types.ERC20Token{
		Amount:   sdk.NewInt(12),
		Contract: tokenETHAddr,
	}

	ethClaim := types.MsgDepositClaim{
		EventNonce:     myNonce,
		TokenContract:  myErc20.Contract,
		Amount:         myErc20.Amount,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   myOrchestratorAddr.String(),
	}

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err := h(ctx, &ethClaim)
	require.NoError(t, err)
	// and claim persisted
	claimFound := k.HasClaim(ctx, types.CLAIM_TYPE_DEPOSIT, myNonce, myValAddr, &ethClaim)
	assert.True(t, claimFound)
	// and attestation persisted
	a := k.GetAttestation(ctx, myNonce, &ethClaim)
	require.NotNil(t, a)
	// and vouchers added to the account
	balance := keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance)

	// Test to reject duplicate deposit
	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err = h(ctx, &ethClaim)
	// then
	require.Error(t, err)
	balance = keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance)

	// Test to reject skipped nonce
	ethClaim = types.MsgDepositClaim{
		EventNonce:     uint64(3),
		TokenContract:  tokenETHAddr,
		Amount:         sdk.NewInt(12),
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   myOrchestratorAddr.String(),
	}

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err = h(ctx, &ethClaim)
	// then
	require.Error(t, err)
	balance = keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance)

	// Test to finally accept consecutive nonce
	ethClaim = types.MsgDepositClaim{
		EventNonce:     uint64(2),
		Amount:         sdk.NewInt(13),
		TokenContract:  tokenETHAddr,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   myOrchestratorAddr.String(),
	}

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err = h(ctx, &ethClaim)
	// then
	require.NoError(t, err)
	balance = keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 25)}, balance)
}

func TestHandleCreateEthereumClaimsMultiValidator(t *testing.T) {
	var (
		orchestratorAddr1, _ = sdk.AccAddressFromBech32("cosmos1dg55rtevlfxh46w88yjpdd08sqhh5cc3xhkcej")
		orchestratorAddr2, _ = sdk.AccAddressFromBech32("cosmos164knshrzuuurf05qxf3q5ewpfnwzl4gj4m4dfy")
		orchestratorAddr3, _ = sdk.AccAddressFromBech32("cosmos193fw83ynn76328pty4yl7473vg9x86alq2cft7")
		myCosmosAddr, _      = sdk.AccAddressFromBech32("cosmos16ahjkfqxpp6lvfy9fpfnfjg39xr96qett0alj5")
		valAddr1             = sdk.ValAddress(orchestratorAddr1) // revisit when proper mapping is impl in keeper
		valAddr2             = sdk.ValAddress(orchestratorAddr2) // revisit when proper mapping is impl in keeper
		valAddr3             = sdk.ValAddress(orchestratorAddr3) // revisit when proper mapping is impl in keeper
		myNonce              = uint64(1)
		anyETHAddr           = "0xf9613b532673Cc223aBa451dFA8539B87e1F666D"
		tokenETHAddr         = "0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e"
		myBlockTime          = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
	)
	k, ctx, keepers := keeper.CreateTestEnv(t)
	k.StakingKeeper = keeper.NewStakingKeeperMock(valAddr1, valAddr2, valAddr3)
	h := NewHandler(k)

	myErc20 := types.ERC20Token{
		Amount:   sdk.NewInt(12),
		Contract: tokenETHAddr,
	}

	ethClaim1 := types.MsgDepositClaim{
		EventNonce:     myNonce,
		TokenContract:  myErc20.Contract,
		Amount:         myErc20.Amount,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   orchestratorAddr1.String(),
	}
	ethClaim2 := types.MsgDepositClaim{
		EventNonce:     myNonce,
		TokenContract:  myErc20.Contract,
		Amount:         myErc20.Amount,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   orchestratorAddr2.String(),
	}
	ethClaim3 := types.MsgDepositClaim{
		EventNonce:     myNonce,
		TokenContract:  myErc20.Contract,
		Amount:         myErc20.Amount,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   orchestratorAddr3.String(),
	}

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err := h(ctx, &ethClaim1)
	require.NoError(t, err)
	// and claim persisted
	claimFound1 := k.HasClaim(ctx, types.CLAIM_TYPE_DEPOSIT, myNonce, valAddr1, &ethClaim1)
	assert.True(t, claimFound1)
	// and attestation persisted
	a1 := k.GetAttestation(ctx, myNonce, &ethClaim1)
	require.NotNil(t, a1)
	// and vouchers not yet added to the account
	balance1 := keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.NotEqual(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance1)

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err = h(ctx, &ethClaim2)
	require.NoError(t, err)

	// and claim persisted
	claimFound2 := k.HasClaim(ctx, types.CLAIM_TYPE_DEPOSIT, myNonce, valAddr1, &ethClaim2)
	assert.True(t, claimFound2)
	// and attestation persisted
	a2 := k.GetAttestation(ctx, myNonce, &ethClaim1)
	require.NotNil(t, a2)
	// and vouchers now added to the account
	balance2 := keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance2)

	// when
	ctx = ctx.WithBlockTime(myBlockTime)
	_, err = h(ctx, &ethClaim3)
	require.NoError(t, err)

	// and claim persisted
	claimFound3 := k.HasClaim(ctx, types.CLAIM_TYPE_DEPOSIT, myNonce, valAddr1, &ethClaim2)
	assert.True(t, claimFound3)
	// and attestation persisted
	a3 := k.GetAttestation(ctx, myNonce, &ethClaim1)
	require.NotNil(t, a3)
	// and no additional added to the account
	balance3 := keepers.BankKeeper.GetAllBalances(ctx, myCosmosAddr)
	assert.Equal(t, sdk.Coins{sdk.NewInt64Coin("peggy/0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e", 12)}, balance3)
}

func TestPackAndUnpackClaims(t *testing.T) {
	var (
		myOrchestratorAddr sdk.AccAddress = make([]byte, sdk.AddrLen)
		myCosmosAddr, _                   = sdk.AccAddressFromBech32("cosmos16ahjkfqxpp6lvfy9fpfnfjg39xr96qett0alj5")
		myNonce                           = uint64(1)
		anyETHAddr                        = "0xf9613b532673Cc223aBa451dFA8539B87e1F666D"
		tokenETHAddr                      = "0x0bc529c00c6401aef6d220be8c6ea1667f6ad93e"
	)

	myErc20 := types.ERC20Token{
		Amount:   sdk.NewInt(12),
		Contract: tokenETHAddr,
	}

	ethClaim := types.MsgDepositClaim{
		EventNonce:     myNonce,
		TokenContract:  myErc20.Contract,
		Amount:         myErc20.Amount,
		EthereumSender: anyETHAddr,
		CosmosReceiver: myCosmosAddr.String(),
		Orchestrator:   myOrchestratorAddr.String(),
	}

	packed, err1 := types.PackEthereumClaim(&ethClaim)
	require.NoError(t, err1)

	unpacked, err2 := types.UnpackEthereumClaim(packed)
	require.NoError(t, err2)
	require.Equal(t, &ethClaim, unpacked)

}

// depreciated, this and all functions related to bridge signature submission should be deleted.
// func TestHandleBridgeSignatureSubmission(t *testing.T) {
// 	var (
// 		myOrchestratorAddr sdk.AccAddress = make([]byte, sdk.AddrLen)
// 		myValAddr                         = sdk.ValAddress(myOrchestratorAddr) // revisit when proper mapping is impl in keeper
// 		myBlockTime                       = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
// 	)

// 	privKey, err := ethCrypto.HexToECDSA("0x2c7dd57db9fda0ea1a1428dcaa4bec1ff7c3bd7d1a88504754e0134b77badf57"[2:])
// 	require.NoError(t, err)

// 	specs := map[string]struct {
// 		setup  func(ctx sdk.Context, k Keeper) MsgBridgeSignatureSubmission
// 		expErr bool
// 	}{
// 		"SignedMultiSigUpdate good": {
// 			setup: func(ctx sdk.Context, k Keeper) MsgBridgeSignatureSubmission {
// 				v := k.SetValsetRequest(ctx)
// 				validSig, err := types.NewEthereumSignature(v.GetCheckpoint(), privKey)
// 				require.NoError(t, err)
// 				return MsgBridgeSignatureSubmission{
// 					SignType:          types.SignTypeOrchestratorSignedMultiSigUpdate,
// 					Nonce:             v.Nonce,
// 					Orchestrator:      myOrchestratorAddr,
// 					EthereumSignature: validSig,
// 				}
// 			},
// 		},
// 		"SignedWithdrawBatch good": {
// 			setup: func(ctx sdk.Context, k Keeper) MsgBridgeSignatureSubmission {
// 				vouchers := keeper.MintVouchersFromAir(t, ctx, k, myOrchestratorAddr, types.NewERC20Token(12, "any", types.NewEthereumAddress("0x4251ed140bf791c4112bb61fcb6e72f927e8fef2")))
// 				require.NoError(t, err)
// 				// with a transaction
// 				k.AddToOutgoingPool(ctx, myOrchestratorAddr, types.NewEthereumAddress("0xb5f728530fe1477ba8b780823a2d48f367fc9fc2"), vouchers, sdk.NewInt64Coin(vouchers.Denom, 0))
// 				voucherDenom, err := types.AsVoucherDenom(vouchers.Denom)
// 				require.NoError(t, err)
// 				// in a batch
// 				b, err := k.BuildOutgoingTXBatch(ctx, voucherDenom, 10)
// 				require.NoError(t, err)
// 				// and a multisig observed
// 				v := k.SetValsetRequest(ctx)
// 				att, err := k.AddClaim(ctx, types.ClaimTypeEthereumBridgeMultiSigUpdate, v.Nonce, myValAddr, types.SignedCheckpoint{Checkpoint: v.GetCheckpoint()})
// 				require.NoError(t, err)
// 				require.Equal(t, types.ProcessStatusProcessed, att.Status)
// 				// create signature
// 				checkpoint, err := b.GetCheckpoint()
// 				require.NoError(t, err)
// 				validSig, err := types.NewEthereumSignature(checkpoint, privKey)
// 				require.NoError(t, err)
// 				return MsgBridgeSignatureSubmission{
// 					SignType:          types.SignTypeOrchestratorSignedWithdrawBatch,
// 					Nonce:             b.Nonce,
// 					Orchestrator:      myOrchestratorAddr,
// 					EthereumSignature: validSig,
// 				}
// 			},
// 		},
// 	}
// 	for msg, spec := range specs {
// 		t.Run(msg, func(t *testing.T) {
// 			k, ctx, _ := keeper.CreateTestEnv(t)
// 			k.StakingKeeper = keeper.NewStakingKeeperMock(myValAddr)
// 			h := NewHandler(k)
// 			k.SetEthAddress(ctx, myValAddr, types.NewEthereumAddress("0xbd5d7df0349ff9671e36ec5545e849cbb93ac7fa"))

// 			// when
// 			ctx = ctx.WithBlockTime(myBlockTime)
// 			msg := spec.setup(ctx, k)
// 			_, err = h(ctx, msg)
// 			if spec.expErr {
// 				assert.Error(t, err)
// 				return
// 			}
// 			// then
// 			require.NoError(t, err)
// 			// and approval persisted
// 			sigFound := k.HasBridgeApprovalSignature(ctx, msg.SignType, msg.Nonce, myValAddr)
// 			assert.True(t, sigFound)
// 		})
// 	}
// }
