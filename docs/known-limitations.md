# Known limitations (first public release)

What a user of the first Phorepoc images should know. The device releases link here.

## Both boards

- **The first boot is slow** (about 50 s to the home screen on a fresh C:); later boots are faster.
- **Video in third-party apps and games needs the codec pack** (`pheffmpeg`, installed as a SIS from Catalogue). Without
  it, MP4 video plays without sound.
- **Qt apps need Qt 4.8.7**, installed as a SIS from Catalogue (it is not part of the image). Example: 10tons' King
  Oddball and Sparkle.
- **No web browser in the image yet**: the stock BrowserNG is not included; a browser comes later as its own package.
- **No camera support.**
- The kernel reports a wrong nanokernel tick period through the HAL (`ENanoTickPeriod` = 10000); apps that time
  themselves from it run at the wrong pace.

## PC (virtual machines)

- **No Wi-Fi** (the WLAN engine is left out); networking is the emulated Intel e1000 card.
- No 3D (OpenGL ES) yet; apps that need it do not start.
- First boot of the `.ova` in a desktop hypervisor has been checked on one host only.

## ARM phone

- **SD card:** use a card of 32 GB or more. C: is a fixed 1 GiB; E: takes the rest of the card and is sized and formatted
  on the first boot, which makes that boot longer. E: is FAT32, so a single file cannot be larger than 4 GB.
- Screen composition runs on the CPU (the GPU composes nothing yet): full-screen animations and video use more CPU than
  they will later.
- The rotated hardware video overlay is not done; landscape video is composed in software.
