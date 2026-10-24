# Known limitations (first public release)

What a user of the first Phorepoc images should know. The device releases link here.

## Both boards

- **The first boot is slow** (about 50 s to the home screen on a fresh C:); later boots are faster.
- **Video in third-party apps and games needs the codec pack** (`pheffmpeg`, installed as a SIS from Get Apps). Without
  it, MP4 video plays without sound.
- **Qt apps need Qt 4.8.7**, installed as a SIS from Get Apps (it is not part of the image). Example: 10tons' King
  Oddball and Sparkle.
- **No web browser in the image yet**: the stock BrowserNG is not included; a browser comes later as its own package.
- **No camera support.**
- The kernel reports a wrong nanokernel tick period through the HAL (`ENanoTickPeriod` = 10000); apps that time
  themselves from it run at the wrong pace.

## PC (QEMU, VirtualBox, VMware)

- **No Wi-Fi** (the WLAN engine is left out); networking is the emulated Intel e1000 card.
- No 3D (OpenGL ES) yet; apps that need it do not start.
- VirtualBox first boot on the `.ova` has been checked on one host only.

## PinePhone

- Screen composition runs on the CPU (the GPU composes nothing yet): full-screen animations and video use more CPU than
  they will later.
- The rotated hardware video overlay is not done; landscape video is composed in software.
- Games with a landscape screen run; some ignore very short taps (under ~0.2 s).
