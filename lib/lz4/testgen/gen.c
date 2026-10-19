// gen.c -- host test generator for the phone's LZ4 encoder (phlz4.h, unmodified: the PinePhone debug link in
// adaptation-a64, usbcc/). Writes ../testdata/cases.bin for lz4_test.go, and with a raw RGB file argument also reports
// live-view numbers.
//   gcc -O2 gen.c -I<adaptation-a64>/usbcc -o gen && ./gen ../testdata/cases.bin
// Cases: sizes 0..40 and then up to 3072 over random / runs / repeated phrases / gradients / screen-like tiles, and the
// table is deliberately reused across cases (stale entries).
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "phlz4.h"

static unsigned rs = 12345;
static unsigned rnd(void) { rs = rs * 1664525u + 1013904223u; return rs >> 8; }

static void fill(unsigned char* b, int n, int kind)
	{
	for (int i = 0; i < n; ++i)
		switch (kind)
			{
		case 0: b[i] = (unsigned char)rnd(); break;
		case 1: b[i] = (unsigned char)((i / 37) * 11); break;
		case 2: b[i] = "the quick brown fox jumps over "[i % 31]; break;
		case 3: b[i] = (unsigned char)(i / 3 + (i % 3) * 40); break;		// gradient, RGB-like
		case 4: b[i] = (rnd() & 7) ? 0xE0 : (unsigned char)rnd(); break;	// mostly flat with noise
		default: b[i] = (i % 3 == 0) ? 0x20 : 0x80; break;
			}
	}

int main(int argc, char** argv)
	{
	static TPhLz4Table t;
	static unsigned char raw[4096], comp[PHLZ4_BOUND(4096)];
	FILE* f = fopen(argc > 1 ? argv[1] : "cases.bin", "wb");
	int cases = 0, bad = 0;
	if (!f) return 1;
	for (int n = 0; n <= 3072; n += (n < 40 ? 1 : 29))
		for (int kind = 0; kind < 6; ++kind)
			{
			fill(raw, n, kind);
			int cl = PhLz4Compress(raw, n, comp, &t);
			if (cl > (int)PHLZ4_BOUND(n)) { ++bad; continue; }
			unsigned h[2] = {(unsigned)n, (unsigned)cl};
			fwrite(h, 4, 2, f); fwrite(raw, 1, n, f); fwrite(comp, 1, cl, f);
			++cases;
			}
	fclose(f);
	printf("%d cases, %d bound violations\n", cases, bad);
	if (argc > 3)	// gen cases.bin frame.rgb W H [frame2.rgb]: the live-view numbers (32x32 tiles, 24-bit RGB)
		{
		int w = atoi(argv[3]), h = atoi(argv[4]);
		FILE* a = fopen(argv[2], "rb");
		unsigned char* A = (unsigned char*)malloc(w * h * 3);
		if (!a || fread(A, 3, w * h, a) != (size_t)(w * h)) { printf("bad rgb\n"); return 1; }
		fclose(a);
		unsigned char* B = NULL;
		if (argc > 5)
			{
			B = (unsigned char*)malloc(w * h * 3);
			a = fopen(argv[5], "rb");
			if (!a || fread(B, 3, w * h, a) != (size_t)(w * h)) { printf("bad rgb2\n"); return 1; }
			fclose(a);
			}
		long raw1 = 0, key = 0, diffb = 0; int tiles = 0, changed = 0;
		for (int ty = 0; ty * 32 < h; ++ty)
			for (int tx = 0; tx * 32 < w; ++tx)
				{
				int tw = w - tx * 32 < 32 ? w - tx * 32 : 32, th = h - ty * 32 < 32 ? h - ty * 32 : 32;
				static unsigned char tile[3072], tile2[3072];
				for (int y = 0; y < th; ++y)
					{
					memcpy(tile + y * tw * 3, A + ((ty * 32 + y) * w + tx * 32) * 3, tw * 3);
					if (B) memcpy(tile2 + y * tw * 3, B + ((ty * 32 + y) * w + tx * 32) * 3, tw * 3);
					}
				int c = PhLz4Compress(tile, tw * th * 3, comp, &t);
				if (c > tw * th * 3) c = tw * th * 3;	// stored raw
				raw1 += tw * th * 3; key += 4 + c; ++tiles;
				if (B && memcmp(tile, tile2, tw * th * 3))
					{
					int c2 = PhLz4Compress(tile2, tw * th * 3, comp, &t);
					if (c2 > tw * th * 3) c2 = tw * th * 3;
					diffb += 4 + c2; ++changed;
					}
				}
		printf("frame %dx%d: raw %ld B, keyframe tiles %ld B (%d tiles, %.1f%%)", w, h, raw1, key, tiles, 100.0 * key / raw1);
		if (B) printf("; diff: %d of %d tiles changed, %ld B", changed, tiles, diffb);
		printf("\n");
		}
	return bad != 0;
	}
