// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phedbg_proto.cpp -- base64, percent-encoding and field parsing for the phedbg line protocol v1.
#include "phedbg.h"
#include <utf.h>

static const TUint8 KB64[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

static void Grow(RBuf8& aBuf, TInt aMore)
	{
	if (aBuf.Length() + aMore > aBuf.MaxLength())
		aBuf.ReAllocL(aBuf.Length() + aMore + 256);
	}

void PheDbgProto::Base64EncodeL(const TDesC8& aIn, RBuf8& aOut)
	{
	Grow(aOut, (aIn.Length() + 2) / 3 * 4);
	TInt i = 0;
	for (; i + 2 < aIn.Length(); i += 3)
		{
		TUint v = (aIn[i] << 16) | (aIn[i + 1] << 8) | aIn[i + 2];
		aOut.Append(KB64[(v >> 18) & 63]); aOut.Append(KB64[(v >> 12) & 63]);
		aOut.Append(KB64[(v >> 6) & 63]); aOut.Append(KB64[v & 63]);
		}
	TInt rest = aIn.Length() - i;
	if (rest)
		{
		TUint v = aIn[i] << 16;
		if (rest == 2)
			v |= aIn[i + 1] << 8;
		aOut.Append(KB64[(v >> 18) & 63]); aOut.Append(KB64[(v >> 12) & 63]);
		aOut.Append(rest == 2 ? KB64[(v >> 6) & 63] : '=');
		aOut.Append('=');
		}
	}

static TInt B64Val(TUint8 c)
	{
	if (c >= 'A' && c <= 'Z') return c - 'A';
	if (c >= 'a' && c <= 'z') return c - 'a' + 26;
	if (c >= '0' && c <= '9') return c - '0' + 52;
	if (c == '+') return 62;
	if (c == '/') return 63;
	return -1;
	}

TInt PheDbgProto::Base64Decode(const TDesC8& aIn, TDes8& aOut)
	{
	TUint acc = 0;
	TInt bits = 0;
	for (TInt i = 0; i < aIn.Length(); i++)
		{
		TUint8 c = aIn[i];
		if (c == '=' || c == '\r' || c == ' ')
			continue;
		TInt v = B64Val(c);
		if (v < 0)
			return KErrCorrupt;
		acc = (acc << 6) | v;
		bits += 6;
		if (bits >= 8)
			{
			bits -= 8;
			if (aOut.Length() == aOut.MaxLength())
				return KErrOverflow;
			aOut.Append((TUint8)(acc >> bits));
			}
		}
	return KErrNone;
	}

void PheDbgProto::PercentEncodeL(const TDesC& aIn, RBuf8& aOut)
	{
	HBufC8* utf = CnvUtfConverter::ConvertFromUnicodeToUtf8L(aIn);
	CleanupStack::PushL(utf);
	Grow(aOut, utf->Length() * 3);
	for (TInt i = 0; i < utf->Length(); i++)
		{
		TUint8 c = (*utf)[i];
		if (c == '%' || c == ' ' || c == '=' || c < 0x20)
			{
			aOut.Append('%');
			aOut.AppendNumFixedWidthUC(c, EHex, 2);
			}
		else
			aOut.Append(c);
		}
	CleanupStack::PopAndDestroy(utf);
	}

static TInt Hex(TUint8 c)
	{
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
	}

void PheDbgProto::PercentDecodeL(const TDesC8& aIn, RBuf& aOut)
	{
	RBuf8 raw;
	raw.CreateL(aIn.Length());
	CleanupClosePushL(raw);
	for (TInt i = 0; i < aIn.Length(); i++)
		{
		TUint8 c = aIn[i];
		if (c == '%' && i + 2 < aIn.Length() + 0 && Hex(aIn[i + 1]) >= 0 && Hex(aIn[i + 2]) >= 0)
			{
			raw.Append((TUint8)(Hex(aIn[i + 1]) * 16 + Hex(aIn[i + 2])));
			i += 2;
			}
		else
			raw.Append(c);
		}
	HBufC* u = CnvUtfConverter::ConvertToUnicodeFromUtf8L(raw);
	aOut.Close();
	aOut.Assign(u);
	CleanupStack::PopAndDestroy(&raw);
	}

TBool PheDbgProto::Field(const TDesC8& aLine, const TDesC8& aKey, TPtrC8& aValue)
	{
	TLex8 lex(aLine);
	while (!lex.Eos())
		{
		TPtrC8 tok = lex.NextToken();
		TInt eq = tok.Locate('=');
		if (eq > 0 && tok.Left(eq) == aKey)
			{
			aValue.Set(tok.Mid(eq + 1));
			return ETrue;
			}
		}
	return EFalse;
	}
