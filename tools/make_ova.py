#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""make_ova -- package the x86-64 image as a VirtualBox appliance (.ova). Runs on Linux in the toolchain container
(ph image vbox); needs neither VirtualBox nor root: qemu-img for the disk, tar and an OVF 1.0 descriptor written here.

The appliance is a PC with the kernel ISO (ph image kernel) in its DVD drive and a blank, formatted C: disk:

    IDE primary master    C: disk (FAT32, streamOptimized VMDK)   persistent: the phone's user data
    IDE secondary master  DVD drive with phorepoc-ekern.iso        GRUB, the kernel and the ROM
    Intel PRO/1000 (82545EM, which the kernel's e1000 driver takes), 2 CPUs with the I/O APIC, AC'97 audio

The archive holds the .ovf first (as the OVF specification wants), then the SHA-256 manifest, the disk and the ISO.

    make_ova.py [-n name] [-m MB RAM] [-c cpus] [-d MB of C:] [-o file.ova]
"""
import argparse
import hashlib
import os
import shutil
import subprocess
import sys
import tarfile
from xml.sax.saxutils import escape

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD, OS_ROOT, ADAPTATIONS  # noqa: E402

OVF_NS = 'xmlns="http://schemas.dmtf.org/ovf/envelope/1" ' \
         'xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" ' \
         'xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" ' \
         'xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData" ' \
         'xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"'


def item(rtype, name, instance, desc=None, subtype=None, parent=None, address=None, on_parent=None, host=None,
         quantity=None, units=None, automatic=None):
    """One rasd:Item of the VirtualHardwareSection; fields in the order the schema wants (alphabetical)."""
    f = []
    if address is not None:
        f.append(f"<rasd:Address>{address}</rasd:Address>")
    if automatic is not None:
        f.append(f"<rasd:AutomaticAllocation>{automatic}</rasd:AutomaticAllocation>")
    if desc:
        f.append(f"<rasd:Description>{escape(desc)}</rasd:Description>")
    f.append(f"<rasd:ElementName>{escape(name)}</rasd:ElementName>")
    if host:
        f.append(f"<rasd:HostResource>{host}</rasd:HostResource>")
    f.append(f"<rasd:InstanceID>{instance}</rasd:InstanceID>")
    if on_parent is not None:
        f.append(f"<rasd:AddressOnParent>{on_parent}</rasd:AddressOnParent>")
    if parent is not None:
        f.append(f"<rasd:Parent>{parent}</rasd:Parent>")
    f.append(f"<rasd:ResourceType>{rtype}</rasd:ResourceType>")
    if subtype:
        f.append(f"<rasd:ResourceSubType>{subtype}</rasd:ResourceSubType>")
    if units:
        f.append(f"<rasd:AllocationUnits>{units}</rasd:AllocationUnits>")
    if quantity is not None:
        f.append(f"<rasd:VirtualQuantity>{quantity}</rasd:VirtualQuantity>")
    return "      <Item>\n        " + "\n        ".join(f) + "\n      </Item>\n"


def ovf(name, mem, cpus, disk_file, disk_bytes, disk_cap, iso_file, iso_bytes):
    """The OVF 1.0 descriptor."""
    items = (
        item(3, f"{cpus} virtual CPU(s)", 1, "Number of virtual CPUs", quantity=cpus, units="hertz * 10^6")
        + item(4, f"{mem} MB of memory", 2, "Memory Size", quantity=mem, units="byte * 2^20")
        + item(5, "ideController0", 3, "IDE Controller", subtype="PIIX4", address=0)
        + item(17, "disk1", 4, "C: disk", parent=3, on_parent=0, host="ovf:/disk/vmdisk1")
        + item(15, "cdrom1", 5, "Kernel and ROM image (GRUB)", parent=3, on_parent=2, host="ovf:/file/file2",
               automatic="true")
        + item(10, "Ethernet adapter on 'NAT'", 6, "Intel PRO/1000", subtype="E1000", automatic="true")
        + item(35, "sound", 7, "Intel AC'97 audio", subtype="ensoniq1371", automatic="true")
    )
    return f"""<?xml version="1.0"?>
<Envelope ovf:version="1.0" xml:lang="en-US" {OVF_NS}>
  <References>
    <File ovf:id="file1" ovf:href="{disk_file}" ovf:size="{disk_bytes}"/>
    <File ovf:id="file2" ovf:href="{iso_file}" ovf:size="{iso_bytes}"/>
  </References>
  <DiskSection>
    <Info>Virtual disk information</Info>
    <Disk ovf:capacity="{disk_cap}" ovf:diskId="vmdisk1" ovf:fileRef="file1"
          ovf:format="http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"/>
  </DiskSection>
  <NetworkSection>
    <Info>Logical networks</Info>
    <Network ovf:name="NAT"><Description>NAT</Description></Network>
  </NetworkSection>
  <VirtualSystem ovf:id="{escape(name)}">
    <Info>Phorepoc, the Symbian OS continuation, on x86-64</Info>
    <Name>{escape(name)}</Name>
    <OperatingSystemSection ovf:id="102">
      <Info>The kind of installed guest operating system</Info>
      <Description>Other_64</Description>
    </OperatingSystemSection>
    <VirtualHardwareSection>
      <Info>Virtual hardware requirements</Info>
      <System>
        <vssd:ElementName>Virtual Hardware Family</vssd:ElementName>
        <vssd:InstanceID>0</vssd:InstanceID>
        <vssd:VirtualSystemIdentifier>{escape(name)}</vssd:VirtualSystemIdentifier>
        <vssd:VirtualSystemType>virtualbox-2.2</vssd:VirtualSystemType>
      </System>
{items}    </VirtualHardwareSection>
  </VirtualSystem>
</Envelope>
"""


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("-n", "--name", default="Phorepoc")
    ap.add_argument("-m", "--mem", type=int, default=1024, help="MB of RAM (the ROM lives in RAM: 1024)")
    ap.add_argument("-c", "--cpus", type=int, default=2)
    ap.add_argument("-d", "--disk", type=int, default=256, help="MB of the C: disk")
    ap.add_argument("-o", "--out", default=None, help="default: <build>/x86/<name>.ova")
    a = ap.parse_args()

    x86 = os.path.join(BUILD, "x86")
    iso = os.path.join(x86, "ekern", "phorepoc-ekern.iso")
    if not os.path.isfile(iso):
        sys.exit(f"no {iso} (ph image kernel first)")
    if not shutil.which("qemu-img"):
        sys.exit("qemu-img not found (the toolchain image needs qemu-utils)")
    mkdisk = os.path.join(OS_ROOT, "tools", "phorepoc-x86", "make_disk.sh")
    if not os.path.isfile(mkdisk):
        sys.exit(f"no {mkdisk} (board pc: adaptation-x86-64)")

    work = os.path.join(x86, "ova")
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work)
    raw = os.path.join(work, "c.img")
    # the blank C: disk: an MBR, one FAT32 partition, the Data folder a phone's C: has
    env = dict(os.environ, KOUT=x86, IMG=raw, LABEL="PHOREPOC")
    subprocess.run(["bash", mkdisk, str(a.disk)], env=env, check=True, stdout=subprocess.DEVNULL)
    disk_file = "phorepoc-c.vmdk"
    vmdk = os.path.join(work, disk_file)
    subprocess.run(["qemu-img", "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=streamOptimized", raw, vmdk],
                   check=True)
    os.unlink(raw)

    iso_file = "phorepoc-ekern.iso"
    descriptor = ovf(a.name, a.mem, a.cpus, disk_file, os.path.getsize(vmdk), a.disk * 1024 * 1024,
                     iso_file, os.path.getsize(iso))
    ovf_file = a.name + ".ovf"
    with open(os.path.join(work, ovf_file), "w", newline="\n") as f:
        f.write(descriptor)
    mf_file = a.name + ".mf"
    files = {ovf_file: os.path.join(work, ovf_file), disk_file: vmdk, iso_file: iso}
    with open(os.path.join(work, mf_file), "w", newline="\n") as f:
        for n, p in files.items():
            f.write(f"SHA256 ({n})= {sha256(p)}\n")

    out = a.out or os.path.join(x86, a.name + ".ova")
    # the OVF first, then the manifest, then the files it lists (the order the OVF specification requires)
    with tarfile.open(out, "w", format=tarfile.USTAR_FORMAT) as t:
        for n, p in [(ovf_file, files[ovf_file]), (mf_file, os.path.join(work, mf_file)),
                     (disk_file, vmdk), (iso_file, iso)]:
            ti = t.gettarinfo(p, arcname=n)
            ti.uid = ti.gid = 0
            ti.uname = ti.gname = ""
            with open(p, "rb") as f:
                t.addfile(ti, f)
    print(f"ova ok: {os.path.getsize(out)} {out}")


if __name__ == "__main__":
    main()
