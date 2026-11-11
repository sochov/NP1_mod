# APN保存プロファイルの読取りと復旧用保管

## 範囲

`modem.ReadGobiNetProfiles(ctx)` は固定の3GPP profile 1/2を読み取る。既存NAS workerからは呼ばず、rootの単発helperとして明示的に実行する。現在接続のAPNを返すGet Current Settingsと、モデムに保存されたプロファイルは別の情報である。公開JSONの `current` は常にfalse。

独立WDS(service 1) CIDを1つだけ作り、tx5/profile1、tx6/profile2のGet Profile Settings（0x002b）を順に送る。request TLV01は `[profileType=0,index=1|2]`。arbitrary index・Create/Modify Profile・Start/Stop Network・Mux Bind・IP family設定は実装しない。driver version/srcversion/SHA256、USB実体、既存FD所有者を確認し、自分のFDを通常closeする。WDA(service 0x1a)の特殊解放経路は使わない。

requestとresponse定義は[公式libqmi WDS schema](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-wds.json)に基づく。PDPはTLV11、APNは14、usernameは1b、passwordは1c、認証は1d。[公式enum定義](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-wds.h)ではPDP 0=IPv4/1=PPP/2=IPv6/3=IPv4v6/4=non-IP、認証0=none/1=PAP/2=CHAP/3=両方。

## 固定queryと時間制限

内部 `fixedQMIRequest` がtx/messageペアからrequest全体を構築し、任意payloadは受け取らない。NASは既存のtx1/0x4f、tx2/0x24、tx3/0x31を維持し、専用NAS wrapperからWDS queryは実行できない。APN currentはtx4/0x2d、saved profileはtx5/6の0x2bだけ。

共通pollは1query最大2秒・非blocking・100msごとのcontext確認・4096byte/frame・無関係frame最大32件。全体contextは8秒。driver open/ioctl/closeのkernel待ちはGoから中断できないため、呼出元の独立helperで外側timeoutを設ける。2queryの失敗は分離し、1件しか取得できない場合はpartialのまま返す。

responseのheader、tx/message、全長、各TLV長、重複、Operation Resultを検証する。省略TLVと長さ0のcredentialを区別する。未知PDP/authや純正への入力に使えない文字列を、既知値や空文字へ置き換えない。

## 公開要約と非公開ファイル

`ProfileRecovery` のraw応答・APN・username・passwordは非公開フィールド。JSON/String/GoStringは要約だけを返す。profile index、取得結果、field数、欠落field名、未知PDP、native用field完全性のboolを含み、APNやcredential値は含まない。

`capture.Save()` はroot専用で、固定 `/overlay/owner-apps/np1-control/private/apn/recovery`（root:0700）へ `profiles-UTC日時-random.json`（root:0600）を保存する。保存対象はdriver fingerprint、取得時刻、profile1/2のQMI応答全体。書込み・fsync後に同一directory内のhardlinkでatomicに公開し、既存captureを上書きしない。公開後directoryもfsyncする。保存時のcaptureは直近2分以内だけを認める。親symlinkは作成前に拒否し、開いたroot boundary以下だけを使う。

`modem.LoadProfileRecovery(filename)` は固定directory内の所定形式のファイル名だけを受け付ける。root所有・0600・通常ファイル・16KiB上限・同一inodeを確認し、JSONのschema/fingerprint、profile順、固定QMI frameを再検証する。raw応答・ファイル本文をAPIや診断ログへ出してはならない。

## native helperへのprivate FD

`capture.WriteNativeProfiles(privateWriter)` はroot専用。両profileにPDP/APN/user/password/authの全TLVがあり、PDP=IPv4(0)、APN非空かつ49 ASCII文字以下、credentialは49文字以下の安全なASCII token、auth0..3の場合だけ316byteの入力を生成する。quote・backslash・制御文字を通さない。対応外PDPや未取得credentialを空へ補って復旧可能とは判定しない。

wire形式は8byte ASCII `NCCAPN1\n` と154byte×2。各recordはindex/auth/PDP/reservedの各1byte、APN/user/passwordの各50byte NUL終端・終端後0埋め。profile1→2の固定順、PDP/reservedは0。writerはprivate pipe/FD専用で、stdout・argv・環境変数に資格情報を置かない。書込み後に一時wire bufferをzeroizeする。

`complete` は必須fieldが取得できたこと、`nativeFieldsComplete` はこの入力形式へ変換できることだけを示す。APNの適用、モデムへの永続化、通信確立、復旧の成功を意味しない。適用は既存所有サービスの入口と結果通知・read-backを別途確認してから行う。

## 検証

Go tests/vetとCGO=0 Linux/ARM64 compileを合格。固定request bytesと任意index/WRITE拒否、既存NAS/current request不変、公開JSON/fmtの非出力、欠落と空値、未知PDP/auth、部分失敗とcancel、TLV/全長、immutable backup・衝突時非上書き・Loadのprovenance/上限、316byte wireと短いwriteを検証した。profile parser fuzzは2秒で63538入力を合格。C側の実parserをWasmで動かすhost testも合格した。

Windows hostでは内部publisherの構造検証に所有者checkerを注入し、公開Save/Load/native writerはLinux root以外で拒否する。Linuxの実0600/root所有・directory fsync・復旧用保存内容は実機スクリプトでread-backが必要。この実装作業では実機QMI query/open・設定変更は実行していない。
