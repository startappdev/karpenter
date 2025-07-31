#!/bin/bash
# Safe script to remove OCI identifiers from files only (not git history)

echo "=== Safe OCI Identifier Cleanup Script ==="
echo
echo "This script will only clean current files, not git history"
echo

# Define the OCI identifiers to remove
COMPARTMENT_ID="aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
TENANCY_ID="aaaaaaaan3x2ydyc7ixtw2kevyivhxjgz5ndphxeauvnwlxcp2pw6kneozca"
POLICY_ID1="aaaaaaaadwv3o6kzq7iulz2htkwfxiyal3esyufodafddr5vtbebaihdpcjq"
POLICY_ID2="aaaaaaaac7mp6nq6idb2p2bpubz4xiq4jebpnc2vs2ycbfj4i7ab53k73j7a"

# Replacement values
COMPARTMENT_REPLACEMENT="<your-compartment-ocid>"
TENANCY_REPLACEMENT="<your-tenancy-ocid>"
POLICY_REPLACEMENT="<your-policy-ocid>"

echo "Step 1: Finding files with OCI identifiers..."

# Function to replace in files
replace_in_file() {
    local file=$1
    local backup_file="${file}.backup"
    
    # Create backup
    cp "$file" "$backup_file"
    
    # Replace compartment IDs
    sed -i '' "s/${COMPARTMENT_ID}/${COMPARTMENT_REPLACEMENT}/g" "$file"
    sed -i '' "s/ocid1\.compartment\.oc1\.\.${COMPARTMENT_ID}/ocid1.compartment.oc1..${COMPARTMENT_REPLACEMENT}/g" "$file"
    
    # Replace tenancy IDs
    sed -i '' "s/${TENANCY_ID}/${TENANCY_REPLACEMENT}/g" "$file"
    sed -i '' "s/ocid1\.tenancy\.oc1\.\.${TENANCY_ID}/ocid1.tenancy.oc1..${TENANCY_REPLACEMENT}/g" "$file"
    
    # Replace policy IDs
    sed -i '' "s/${POLICY_ID1}/${POLICY_REPLACEMENT}/g" "$file"
    sed -i '' "s/${POLICY_ID2}/${POLICY_REPLACEMENT}/g" "$file"
    sed -i '' "s/ocid1\.policy\.oc1\.\.${POLICY_ID1}/ocid1.policy.oc1..${POLICY_REPLACEMENT}/g" "$file"
    sed -i '' "s/ocid1\.policy\.oc1\.\.${POLICY_ID2}/ocid1.policy.oc1..${POLICY_REPLACEMENT}/g" "$file"
    
    # Check if file was modified
    if ! diff -q "$file" "$backup_file" > /dev/null 2>&1; then
        echo "  Updated: $file"
        rm "$backup_file"
    else
        rm "$backup_file"
    fi
}

# Find and process files
echo
echo "Step 2: Replacing OCI identifiers in files..."

# Search for files containing the identifiers
for id in "$COMPARTMENT_ID" "$TENANCY_ID" "$POLICY_ID1" "$POLICY_ID2"; do
    grep -r -l "$id" . --exclude-dir=.git --exclude="*.sh" 2>/dev/null | while read -r file; do
        replace_in_file "$file"
    done
done

# Also check for the OCID format
grep -r -l "ocid1\.\(compartment\|tenancy\|policy\)\.oc1\.\.$\(COMPARTMENT_ID\|TENANCY_ID\|POLICY_ID1\|POLICY_ID2\)" . --exclude-dir=.git --exclude="*.sh" 2>/dev/null | while read -r file; do
    replace_in_file "$file"
done

echo
echo "Step 3: Verification..."

# Verify cleanup
echo "Checking for remaining identifiers..."
FOUND=0
for id in "$COMPARTMENT_ID" "$TENANCY_ID" "$POLICY_ID1" "$POLICY_ID2"; do
    if grep -r "$id" . --exclude-dir=.git --exclude="*.sh" > /dev/null 2>&1; then
        echo "WARNING: Found remaining instances of $id"
        FOUND=1
    fi
done

if [ $FOUND -eq 0 ]; then
    echo "All identifiers have been successfully replaced!"
else
    echo "Some identifiers may still remain. Please check manually."
fi

echo
echo "=== Cleanup Complete ==="
echo
echo "IMPORTANT NEXT STEPS:"
echo "1. Review the changes: git diff"
echo "2. Commit the changes: git add -A && git commit -m 'Remove sensitive OCI identifiers'"
echo "3. Push to remote: git push origin start-io"
echo
echo "For git history cleanup, consider using git-filter-repo or BFG Repo-Cleaner"
echo "as they are safer alternatives to git filter-branch."