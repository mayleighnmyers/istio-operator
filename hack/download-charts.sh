#!/usr/bin/env bash

set -e -x -u -o pipefail

: "${MAISTRA_RELEASE_STREAM:=$1}"
: "${ISTIO_REPO:=$2}"
: "${ISTIO_COMMIT:=$3}"

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
REPO_ROOT=$(dirname "${SCRIPT_DIR}")
MANIFEST_DIR="${REPO_ROOT}/resources/${MAISTRA_RELEASE_STREAM}"
CHARTS_DIR="${MANIFEST_DIR}/charts"
PROFILES_DIR="${MANIFEST_DIR}/profiles"
CONFIG_DIR="${REPO_ROOT}/config"

ISTIO_FILE="${ISTIO_COMMIT}.tar.gz"
ISTIO_URL="${ISTIO_REPO}/archive/${ISTIO_COMMIT}.tar.gz"
WORK_DIR=$(mktemp -d)
EXTRACT_DIR="${ISTIO_REPO##*/}-${ISTIO_COMMIT}"
EXTRACT_CMD="tar zxf ${ISTIO_FILE} ${EXTRACT_DIR}/manifests/charts ${EXTRACT_DIR}/manifests/profiles ${EXTRACT_DIR}/manifests/addons/dashboards"

function downloadIstioManifests() {
  rm -rf "${CHARTS_DIR}"
  mkdir -p "${CHARTS_DIR}"

  rm -rf "${PROFILES_DIR}"
  mkdir -p "${PROFILES_DIR}"

  echo "downloading charts and profiles from ${ISTIO_URL}"
  pushd "${WORK_DIR}"
  curl -LfO "${ISTIO_URL}"

  echo "extracting charts to ${WORK_DIR}/${EXTRACT_DIR}"
  ${EXTRACT_CMD}
  echo "copying charts to ${CHARTS_DIR}"
  cp -rf "${WORK_DIR}"/"${EXTRACT_DIR}"/manifests/charts/* "${CHARTS_DIR}/"
  echo "copying manifests to ${CHARTS_DIR}"
  cp -rf "${WORK_DIR}"/"${EXTRACT_DIR}"/manifests/profiles/* "${PROFILES_DIR}/"

  popd
}

function patchIstioCharts() {
  # NOTE: everything in the patchIstioCharts should be here only temporarily,
  # until we push the required changes upstream
  sed -i '0,/rules:/s//rules:\
- apiGroups: ["security.openshift.io"] \
  resources: ["securitycontextconstraints"] \
  resourceNames: ["privileged"] \
  verbs: ["use"]/' "${CHARTS_DIR}/istio-cni/templates/clusterrole.yaml"
}

function convertIstioProfiles() {
  for profile in "${PROFILES_DIR}"/*.yaml; do
    yq eval -i '.apiVersion="operator.istio.io/v1alpha1"
      | .kind="Istio"
      | del(.metadata)
      | del(.spec.components)
      | del(.spec.meshConfig)
      | del(.spec.hub)
      | del(.spec.tag)' "$profile"
  done
}

function copyCRDs() {
  # Split the YAML file into separate CRD files
  csplit -s --suppress-matched -f "${CONFIG_DIR}/crd/bases/istio-crd" -z "${CHARTS_DIR}/base/crds/crd-all.gen.yaml" '/^---$/' '{*}'

  # To hide istio CRDs in the OpenShift Console, we add them to the intenral-objects annotation in the CSV
  internalObjects=""

  # Rename the split files to <api group>_<resource name>.yaml
  for file in "${CONFIG_DIR}/crd/bases/istio-crd"*; do
    # Extract the group and resource from each CRD
    group=$(grep -oP '^\s*group:\s*\K.*' "$file" | tr -d '[:space:]')
    resource=$(grep -oP '^\s*plural:\s*\K.*' "$file" | tr -d '[:space:]')
    # Add the CRD to the list of internal objects
    internalObjects+="\"${resource}.${group}\","
    # Rename the file to <group>_<resource>.yaml
    mv "$file" "${CONFIG_DIR}/crd/bases/${group}_${resource}.yaml"
  done

  # Remove existing list of CRD files from kustomization.yaml
  sed -i '/resources:/,/#+kubebuilder:scaffold:crdkustomizeresource/ {
    /resources:/n
    /#+kubebuilder:scaffold:crdkustomizeresource/!d
    }' "${CONFIG_DIR}/crd/kustomization.yaml"

  # Create YAML snippet containing list of CRD files
  { cd "${CONFIG_DIR}/crd"; find "bases/"*.yaml | sed 's/^/- /'; } > "${CONFIG_DIR}/crd/crdfiles.yaml"

  # Insert snippet into kustomization.yaml
  sed -i '/resources:/r '"${CONFIG_DIR}/crd/crdfiles.yaml" "${CONFIG_DIR}/crd/kustomization.yaml"

  # Remove snippet file
  rm "${CONFIG_DIR}/crd/crdfiles.yaml"

  # Update internal-objects annotation in CSV
  sed -i "/operators\.operatorframework\.io\/internal-objects/ c\    operators.operatorframework.io/internal-objects: '[${internalObjects%?}]'" "${CONFIG_DIR}/manifests/bases/sailoperator.clusterserviceversion.yaml"
}

downloadIstioManifests
patchIstioCharts
convertIstioProfiles
copyCRDs