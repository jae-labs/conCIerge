# TFLint Configuration
# https://github.com/terraform-linters/tflint/blob/master/docs/user-guide/config.md

config {
  force = false
}

# Ignore unused declarations since some variables/locals are defined for future extensions or testing
rule "terraform_unused_declarations" {
  enabled = false
}
