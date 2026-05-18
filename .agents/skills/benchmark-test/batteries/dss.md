# Dai Stablecoin System Query Battery — Grounded Expected Symbols

All symbols verified against sky-ecosystem/dss source on GitHub (2026-05-18).

## Investigation 1: Vat core accounting and authorization (9 symbols)

Query keyword: `"Vat frob fork grab fold debt collateral urn ilk auth"`
Query intent: `"how does the DSS Vat manage collateral, normalized debt, permissions, and rates"`

Expected symbols:
- `Vat` — src/vat.sol — core CDP accounting database for collateral, debt, and system state
- `Ilk` — src/vat.sol — collateral-type accounting struct with Art, rate, spot, line, and dust
- `Urn` — src/vat.sol — vault accounting struct with locked collateral and normalized debt
- `rely` — src/vat.sol — grants authorization to a ward
- `deny` — src/vat.sol — revokes ward authorization
- `init` — src/vat.sol — initializes a collateral type rate
- `frob` — src/vat.sol — main CDP manipulation function for collateral and debt changes
- `fork` — src/vat.sol — transfers collateral/debt between urns
- `fold` — src/vat.sol — adjusts accumulated rates and system debt

## Investigation 2: Token join adapters (8 symbols)

Query keyword: `"GemJoin DaiJoin join exit slip move mint burn cage"`
Query intent: `"how do DSS join adapters move collateral and Dai between external tokens and Vat balances"`

Expected symbols:
- `GemJoin` — src/join.sol — collateral adapter for moving ERC-20 collateral into and out of Vat accounting
- `DaiJoin` — src/join.sol — Dai adapter for minting/burning Dai against Vat internal balances
- `GemLike` — src/join.sol — minimal collateral token interface used by `GemJoin`
- `DaiLike` — src/join.sol — minimal Dai token interface used by `DaiJoin`
- `VatLike` — src/join.sol — Vat interface used by adapters to call `slip` and `move`
- `join` — src/join.sol — deposits collateral or burns Dai while crediting Vat balances
- `exit` — src/join.sol — withdraws collateral or mints Dai while debiting Vat balances
- `cage` — src/join.sol — disables adapter joins after shutdown

## Investigation 3: Savings and stability fee rates (8 symbols)

Query keyword: `"Pot Jug drip dsr duty base fold rho chi"`
Query intent: `"how does DSS accrue Dai savings rate and collateral stability fees"`

Expected symbols:
- `Pot` — src/pot.sol — Dai Savings Rate accumulator contract
- `Jug` — src/jug.sol — stability-fee accumulator for collateral types
- `drip` — src/pot.sol — accrues the savings rate and updates `chi`
- `drip` — src/jug.sol — accrues stability fees for an ilk and calls Vat `fold`
- `file` — src/pot.sol — updates Pot configuration such as `dsr` or `vow`
- `file` — src/jug.sol — updates Jug base duty, per-ilk duty, or Vow address
- `rho` — src/pot.sol — timestamp of last Pot rate accumulation
- `chi` — src/pot.sol — accumulated savings rate accumulator

## Investigation 4: Liquidation auction flow (9 symbols)

Query keyword: `"Dog Clipper bark kick take redo yank liquidation auction chop hole"`
Query intent: `"how does DSS detect unsafe vaults and run collateral liquidation auctions"`

Expected symbols:
- `Dog` — src/dog.sol — liquidation engine that checks unsafe vaults and starts auctions
- `bark` — src/dog.sol — triggers liquidation for an unsafe urn and kicks a Clipper auction
- `Clipper` — src/clip.sol — collateral auction contract for liquidations
- `kick` — src/clip.sol — starts a new collateral auction
- `take` — src/clip.sol — buys collateral from an active auction
- `redo` — src/clip.sol — resets stale auctions and pays keeper incentives
- `yank` — src/clip.sol — cancels an auction during shutdown
- `upchost` — src/clip.sol — refreshes cached keeper incentive cap
- `file` — src/dog.sol — configures liquidation parameters and clipper addresses

## Investigation 5: Surplus, debt, and global settlement (9 symbols)

Query keyword: `"Vow End cage heal flap flop skim free pack cash settlement"`
Query intent: `"how does DSS handle surplus/debt auctions and global settlement withdrawals"`

Expected symbols:
- `Vow` — src/vow.sol — system surplus and debt settlement buffer
- `fess` — src/vow.sol — records new system debt in the Vow queue
- `flog` — src/vow.sol — matures queued debt for settlement
- `heal` — src/vow.sol — cancels surplus Dai against system debt
- `flop` — src/vow.sol — starts a debt auction
- `flap` — src/vow.sol — starts a surplus auction
- `End` — src/end.sol — global settlement coordinator
- `cage` — src/end.sol — initiates global settlement and cages core modules
- `cash` — src/end.sol — lets users redeem collateral after settlement flow is complete
