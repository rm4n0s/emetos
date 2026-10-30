# gpu-env.sh is sourced by the scripts that run Emetos for tests and proofs (headless-wayland.sh, proof.sh, realinput.sh).
# It keeps everything they start off the NVIDIA card (the user, 2026-10-30: "dont use my nvidia card to test"): EGL and GLX
# load Mesa only (glvnd would try NVIDIA's vendor library first), and the Vulkan loader sees every driver but NVIDIA's.
# internal/testenv/display does the same for the editors the display tests start (display.GPUEnv).

if [[ -f /usr/share/glvnd/egl_vendor.d/50_mesa.json ]]; then
  export __EGL_VENDOR_LIBRARY_FILENAMES=/usr/share/glvnd/egl_vendor.d/50_mesa.json
fi
export __GLX_VENDOR_LIBRARY_NAME=mesa
_emetos_vk=()
for _emetos_icd in /usr/share/vulkan/icd.d/*.json /etc/vulkan/icd.d/*.json; do
  [[ -f "$_emetos_icd" && "$_emetos_icd" != *nvidia* ]] && _emetos_vk+=("$_emetos_icd")
done
if ((${#_emetos_vk[@]})); then
  VK_DRIVER_FILES=$(IFS=:; echo "${_emetos_vk[*]}")
  export VK_DRIVER_FILES VK_ICD_FILENAMES="$VK_DRIVER_FILES"
fi
unset _emetos_vk _emetos_icd
