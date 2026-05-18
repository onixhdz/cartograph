# OpenZeppelin Contracts Query Battery — Grounded Expected Symbols

All symbols verified against OpenZeppelin/openzeppelin-contracts source on GitHub (2026-05-18).

## Investigation 1: ERC-20 token accounting (9 symbols)

Query keyword: `"ERC20 transfer mint burn allowance balances totalSupply"`
Query intent: `"how does OpenZeppelin ERC20 move balances, mint, burn, and spend allowances"`

Expected symbols:
- `ERC20` — contracts/token/ERC20/ERC20.sol — canonical ERC-20 implementation storing balances, allowances, supply, name, and symbol
- `transfer` — contracts/token/ERC20/ERC20.sol — public token transfer entry point
- `transferFrom` — contracts/token/ERC20/ERC20.sol — allowance-consuming transfer entry point
- `_transfer` — contracts/token/ERC20/ERC20.sol — internal transfer validation wrapper
- `_update` — contracts/token/ERC20/ERC20.sol — shared balance/supply mutation path for transfers, minting, and burning
- `_mint` — contracts/token/ERC20/ERC20.sol — internal mint helper that increases supply through `_update`
- `_burn` — contracts/token/ERC20/ERC20.sol — internal burn helper that decreases supply through `_update`
- `_approve` — contracts/token/ERC20/ERC20.sol — internal allowance mutation helper
- `_spendAllowance` — contracts/token/ERC20/ERC20.sol — checks and decrements allowance during `transferFrom`

## Investigation 2: Ownership and role authorization (9 symbols)

Query keyword: `"Ownable AccessControl onlyOwner role grant revoke admin"`
Query intent: `"how do OpenZeppelin contracts enforce owners and role-based permissions"`

Expected symbols:
- `Ownable` — contracts/access/Ownable.sol — base contract for single-owner authorization
- `onlyOwner` — contracts/access/Ownable.sol — modifier that gates functions to the current owner
- `owner` — contracts/access/Ownable.sol — returns the current owner address
- `_checkOwner` — contracts/access/Ownable.sol — internal owner validation used by `onlyOwner`
- `transferOwnership` — contracts/access/Ownable.sol — public ownership handoff function
- `_transferOwnership` — contracts/access/Ownable.sol — internal owner state update and event emission
- `AccessControl` — contracts/access/AccessControl.sol — role-based authorization base contract
- `grantRole` — contracts/access/AccessControl.sol — public role grant entry point
- `_grantRole` — contracts/access/AccessControl.sol — internal role membership mutation helper

## Investigation 3: Proxy upgrade dispatch (8 symbols)

Query keyword: `"proxy delegate implementation ERC1967 upgrade admin transparent"`
Query intent: `"how does OpenZeppelin route proxy calls and perform ERC1967 upgrades"`

Expected symbols:
- `Proxy` — contracts/proxy/Proxy.sol — base proxy that delegates unknown calls to an implementation
- `_delegate` — contracts/proxy/Proxy.sol — low-level assembly delegatecall dispatcher
- `_implementation` — contracts/proxy/Proxy.sol — abstract hook returning the active implementation
- `ERC1967Proxy` — contracts/proxy/ERC1967/ERC1967Proxy.sol — proxy using the ERC-1967 implementation slot
- `ERC1967Utils` — contracts/proxy/ERC1967/ERC1967Utils.sol — library for ERC-1967 slots and upgrade operations
- `upgradeToAndCall` — contracts/proxy/ERC1967/ERC1967Utils.sol — updates implementation and optionally executes setup call
- `TransparentUpgradeableProxy` — contracts/proxy/transparent/TransparentUpgradeableProxy.sol — transparent proxy separating admin and user call paths
- `ProxyAdmin` — contracts/proxy/transparent/ProxyAdmin.sol — Ownable admin contract for transparent proxy upgrades

## Investigation 4: Governor proposal lifecycle (9 symbols)

Query keyword: `"Governor propose queue execute castVote proposal state"`
Query intent: `"how does OpenZeppelin Governor create proposals, collect votes, queue, and execute actions"`

Expected symbols:
- `Governor` — contracts/governance/Governor.sol — abstract governance contract implementing the proposal lifecycle
- `state` — contracts/governance/Governor.sol — computes the current proposal state
- `propose` — contracts/governance/Governor.sol — creates a new proposal with targets, values, calldata, and description
- `queue` — contracts/governance/Governor.sol — queues a successful proposal when a delay backend is configured
- `_queueOperations` — contracts/governance/Governor.sol — internal hook for queueing proposal operations
- `execute` — contracts/governance/Governor.sol — executes queued or successful proposal actions
- `_executeOperations` — contracts/governance/Governor.sol — internal operation execution loop
- `castVote` — contracts/governance/Governor.sol — public voting entry point
- `_castVote` — contracts/governance/Governor.sol — internal vote accounting path

## Investigation 5: Cryptographic verification utilities (9 symbols)

Query keyword: `"ECDSA recover signature hash MerkleProof verify SignatureChecker"`
Query intent: `"how do OpenZeppelin utilities recover signatures and verify cryptographic proofs"`

Expected symbols:
- `ECDSA` — contracts/utils/cryptography/ECDSA.sol — library for ECDSA signature recovery helpers
- `tryRecover` — contracts/utils/cryptography/ECDSA.sol — non-reverting signature recovery variants
- `recover` — contracts/utils/cryptography/ECDSA.sol — reverting signature recovery helpers
- `MessageHashUtils` — contracts/utils/cryptography/MessageHashUtils.sol — library for Ethereum signed message hash formats
- `toEthSignedMessageHash` — contracts/utils/cryptography/MessageHashUtils.sol — creates EIP-191 personal-sign message digests
- `SignatureChecker` — contracts/utils/cryptography/SignatureChecker.sol — validates ECDSA and ERC-1271 signatures
- `isValidSignatureNow` — contracts/utils/cryptography/SignatureChecker.sol — checks signature validity for EOAs or contract signers
- `MerkleProof` — contracts/utils/cryptography/MerkleProof.sol — library for Merkle inclusion proof verification
- `processProof` — contracts/utils/cryptography/MerkleProof.sol — reconstructs a Merkle root from a proof and leaf
